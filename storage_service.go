package main

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

// StorageService interface mendefinisikan operasi bisnis untuk file storage
// Layer service ini mengabstraksi kompleksitas interaksi antara database dan object storage
type StorageService interface {
	UploadFile(ctx context.Context, file *multipart.FileHeader, description string) (*FileMetadata, error)
	GetFile(ctx context.Context, id string) (*FileMetadata, error)
	ListFiles(ctx context.Context, limit, offset int) ([]*FileMetadata, int, error)
	DownloadFile(ctx context.Context, id string) (io.ReadCloser, *FileMetadata, error)
	DeleteFile(ctx context.Context, id string) error
	GeneratePresignedURL(ctx context.Context, id string, expiry time.Duration) (string, error)
}

// storageService adalah implementasi konkret dari StorageService interface
// Struct ini menggabungkan S3 client untuk akses ke RustFS dan repository untuk database
type storageService struct {
	s3Client *s3.Client
	fileRepo FileRepository
	config   *Config
}

// NewStorageService membuat instance baru StorageService
// Dependency injection pattern: semua dependencies di-inject lewat constructor
func NewStorageService(
	s3Client *s3.Client,
	fileRepo FileRepository,
	cfg *Config,
) StorageService {
	return &storageService{
		s3Client: s3Client,
		fileRepo: fileRepo,
		config:   cfg,
	}
}

// UploadFile menghandle proses upload file secara lengkap:
// 1. Validasi file
// 2. Generate object key unik
// 3. Upload ke RustFS object storage
// 4. Simpan metadata ke database
// Jika salah satu langkah gagal, akan dilakukan rollback/cleanup
func (s *storageService) UploadFile(
	ctx context.Context,
	fileHeader *multipart.FileHeader,
	description string,
) (*FileMetadata, error) {
	// Step 1: Validasi ukuran file (max 100MB untuk contoh ini)
	maxSize := int64(100 * 1024 * 1024) // 100 MB
	if fileHeader.Size > maxSize {
		return nil, fmt.Errorf("file size exceeds maximum limit of 100MB")
	}

	// Step 2: Buka file untuk dibaca
	file, err := fileHeader.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open uploaded file: %w", err)
	}
	defer file.Close()

	// Step 3: Generate UUID untuk identifier unik file
	fileID := uuid.New().String()

	// Step 4: Generate object key yang unik dan terorganisir
	// Format: files/{year}/{month}/{uuid}_{original_filename}
	// Struktur folder membantu organisasi dan mencegah collision
	now := time.Now()
	objectKey := fmt.Sprintf(
		"files/%d/%02d/%s_%s",
		now.Year(),
		now.Month(),
		fileID,
		sanitizeFilename(fileHeader.Filename),
	)

	// Step 5: Upload file ke RustFS menggunakan S3 PutObject API
	_, err = s.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.config.RustFS.Bucket),
		Key:         aws.String(objectKey),
		Body:        file,
		ContentType: aws.String(detectContentType(fileHeader.Filename)),
		// Metadata tambahan yang bisa digunakan untuk tracking atau filtering
		Metadata: map[string]string{
			"original-filename": fileHeader.Filename,
			"uploaded-by":       "system", // Bisa diganti dengan user ID dari auth
		},
	})

	if err != nil {
		return nil, fmt.Errorf("failed to upload file to storage: %w", err)
	}

	// Step 6: Simpan metadata ke database
	metadata := &FileMetadata{
		ID:          fileID,
		Filename:    fileHeader.Filename,
		ContentType: detectContentType(fileHeader.Filename),
		Size:        fileHeader.Size,
		ObjectKey:   objectKey,
		Description: description,
		UploadedBy:  "system", // Akan diubah jika ada auth
	}

	err = s.fileRepo.Create(ctx, metadata)
	if err != nil {
		// Jika gagal save ke database, cleanup file yang sudah diupload
		// Ini penting untuk menjaga konsistensi data
		_ = s.deleteFromStorage(ctx, objectKey)
		return nil, fmt.Errorf("failed to save file metadata: %w", err)
	}

	return metadata, nil
}

// GetFile mengambil metadata file berdasarkan ID
func (s *storageService) GetFile(ctx context.Context, id string) (*FileMetadata, error) {
	return s.fileRepo.GetByID(ctx, id)
}

// ListFiles mengambil daftar file dengan pagination
// Return: list files, total count, error
func (s *storageService) ListFiles(
	ctx context.Context,
	limit, offset int,
) ([]*FileMetadata, int, error) {
	// Validasi pagination parameters
	if limit <= 0 {
		limit = 10 // Default limit
	}
	if limit > 100 {
		limit = 100 // Maximum limit untuk prevent overload
	}
	if offset < 0 {
		offset = 0
	}

	// Get list files dari database
	files, err := s.fileRepo.List(ctx, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list files: %w", err)
	}

	// Get total count untuk pagination metadata
	count, err := s.fileRepo.Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count files: %w", err)
	}

	return files, count, nil
}

// DownloadFile mengambil file dari RustFS storage
// Return: file stream, metadata, error
// Caller bertanggung jawab untuk close ReadCloser
func (s *storageService) DownloadFile(
	ctx context.Context,
	id string,
) (io.ReadCloser, *FileMetadata, error) {
	// Step 1: Get metadata dari database
	metadata, err := s.fileRepo.GetByID(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get file metadata: %w", err)
	}

	// Step 2: Download file dari RustFS
	result, err := s.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.config.RustFS.Bucket),
		Key:    aws.String(metadata.ObjectKey),
	})

	if err != nil {
		return nil, nil, fmt.Errorf("failed to download file from storage: %w", err)
	}

	return result.Body, metadata, nil
}

// DeleteFile menghapus file secara lengkap:
// 1. Hapus dari object storage (RustFS)
// 2. Hapus metadata dari database
// Kedua operasi harus berhasil untuk menjaga konsistensi
func (s *storageService) DeleteFile(ctx context.Context, id string) error {
	// Step 1: Get metadata untuk mendapatkan object key
	metadata, err := s.fileRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get file metadata: %w", err)
	}

	// Step 2: Hapus dari object storage
	err = s.deleteFromStorage(ctx, metadata.ObjectKey)
	if err != nil {
		return fmt.Errorf("failed to delete file from storage: %w", err)
	}

	// Step 3: Hapus metadata dari database
	err = s.fileRepo.Delete(ctx, id)
	if err != nil {
		// File sudah terhapus dari storage tapi gagal hapus dari database
		// Ini akan menyebabkan "orphan" record di database
		// Di production, ini harus di-handle dengan background cleanup job
		return fmt.Errorf("failed to delete file metadata: %w", err)
	}

	return nil
}

// GeneratePresignedURL membuat URL temporary untuk download file
// Pre-signed URL berguna untuk:
// - Sharing file tanpa perlu expose credentials
// - Memberikan akses temporary dengan expiry time
// - Direct download tanpa melewati aplikasi server
func (s *storageService) GeneratePresignedURL(
	ctx context.Context,
	id string,
	expiry time.Duration,
) (string, error) {
	// Get metadata untuk mendapatkan object key
	metadata, err := s.fileRepo.GetByID(ctx, id)
	if err != nil {
		return "", fmt.Errorf("failed to get file metadata: %w", err)
	}

	// Generate presigned URL menggunakan S3 Presign client
	presignClient := s3.NewPresignClient(s.s3Client)

	request, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.config.RustFS.Bucket),
		Key:    aws.String(metadata.ObjectKey),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = expiry
	})

	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	return request.URL, nil
}

// deleteFromStorage adalah helper function untuk menghapus object dari RustFS
func (s *storageService) deleteFromStorage(ctx context.Context, objectKey string) error {
	_, err := s.s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.config.RustFS.Bucket),
		Key:    aws.String(objectKey),
	})
	return err
}

// sanitizeFilename membersihkan filename dari karakter yang tidak aman
// Mencegah path traversal dan masalah encoding
func sanitizeFilename(filename string) string {
	// Replace karakter yang tidak aman
	replacer := strings.NewReplacer(
		"..", "",
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	return replacer.Replace(filename)
}

// detectContentType mendeteksi MIME type berdasarkan file extension
// Ini penting untuk proper HTTP response headers
func detectContentType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))

	contentTypes := map[string]string{
		".pdf":  "application/pdf",
		".doc":  "application/msword",
		".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		".xls":  "application/vnd.ms-excel",
		".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		".ppt":  "application/vnd.ms-powerpoint",
		".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		".txt":  "text/plain",
		".csv":  "text/csv",
		".json": "application/json",
		".xml":  "application/xml",
		".zip":  "application/zip",
		".rar":  "application/x-rar-compressed",
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".png":  "image/png",
		".gif":  "image/gif",
		".bmp":  "image/bmp",
		".svg":  "image/svg+xml",
		".mp3":  "audio/mpeg",
		".mp4":  "video/mp4",
		".avi":  "video/x-msvideo",
	}

	if contentType, ok := contentTypes[ext]; ok {
		return contentType
	}

	// Default content type untuk file yang tidak dikenali
	return "application/octet-stream"
}
