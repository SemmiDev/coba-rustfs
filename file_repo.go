package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// FileMetadata merepresentasikan metadata file yang disimpan di database
// Metadata ini terpisah dari file actual yang disimpan di RustFS object storage
// Dengan pola ini, kita bisa melakukan query cepat tanpa harus akses object storage
type FileMetadata struct {
	ID          string    `json:"id"`           // UUID unik untuk setiap file
	Filename    string    `json:"filename"`     // Nama file original (contoh: document.pdf)
	ContentType string    `json:"content_type"` // MIME type (contoh: application/pdf)
	Size        int64     `json:"size"`         // Ukuran file dalam bytes
	ObjectKey   string    `json:"object_key"`   // Key/path file di object storage
	Description string    `json:"description"`  // Deskripsi opsional dari user
	UploadedAt  time.Time `json:"uploaded_at"`  // Waktu upload
	UploadedBy  string    `json:"uploaded_by"`  // User yang upload (untuk future use)
}

// FileRepository interface mendefinisikan operasi database untuk file metadata
// Dengan interface, kita bisa mudah mock untuk unit testing
type FileRepository interface {
	Create(ctx context.Context, file *FileMetadata) error
	GetByID(ctx context.Context, id string) (*FileMetadata, error)
	GetByObjectKey(ctx context.Context, objectKey string) (*FileMetadata, error)
	List(ctx context.Context, limit, offset int) ([]*FileMetadata, error)
	Delete(ctx context.Context, id string) error
	Count(ctx context.Context) (int, error)
}

// fileRepository adalah implementasi konkret dari FileRepository interface
type fileRepository struct {
	db *sql.DB
}

// NewFileRepository membuat instance baru FileRepository
func NewFileRepository(db *sql.DB) FileRepository {
	return &fileRepository{db: db}
}

// Create menyimpan metadata file baru ke database
// Operasi ini dilakukan setelah file berhasil diupload ke RustFS
func (r *fileRepository) Create(ctx context.Context, file *FileMetadata) error {
	query := `
		INSERT INTO files (id, filename, content_type, size, object_key, description, uploaded_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING uploaded_at
	`

	// Gunakan QueryRowContext untuk INSERT dengan RETURNING clause
	// Ini memberi kita timestamp yang di-generate oleh database
	err := r.db.QueryRowContext(
		ctx,
		query,
		file.ID,
		file.Filename,
		file.ContentType,
		file.Size,
		file.ObjectKey,
		file.Description,
		file.UploadedBy,
	).Scan(&file.UploadedAt)

	if err != nil {
		return fmt.Errorf("failed to create file metadata: %w", err)
	}

	return nil
}

// GetByID mengambil metadata file berdasarkan ID
func (r *fileRepository) GetByID(ctx context.Context, id string) (*FileMetadata, error) {
	query := `
		SELECT id, filename, content_type, size, object_key, description, uploaded_at, uploaded_by
		FROM files
		WHERE id = $1
	`

	file := &FileMetadata{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&file.ID,
		&file.Filename,
		&file.ContentType,
		&file.Size,
		&file.ObjectKey,
		&file.Description,
		&file.UploadedAt,
		&file.UploadedBy,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("file not found")
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get file metadata: %w", err)
	}

	return file, nil
}

// GetByObjectKey mengambil metadata file berdasarkan object key di storage
// Fungsi ini berguna saat kita perlu lookup metadata dari object key
func (r *fileRepository) GetByObjectKey(ctx context.Context, objectKey string) (*FileMetadata, error) {
	query := `
		SELECT id, filename, content_type, size, object_key, description, uploaded_at, uploaded_by
		FROM files
		WHERE object_key = $1
	`

	file := &FileMetadata{}
	err := r.db.QueryRowContext(ctx, query, objectKey).Scan(
		&file.ID,
		&file.Filename,
		&file.ContentType,
		&file.Size,
		&file.ObjectKey,
		&file.Description,
		&file.UploadedAt,
		&file.UploadedBy,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("file not found")
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get file metadata: %w", err)
	}

	return file, nil
}

// List mengambil daftar semua file dengan pagination
// Pagination penting untuk performa saat jumlah file banyak
func (r *fileRepository) List(ctx context.Context, limit, offset int) ([]*FileMetadata, error) {
	// Query dengan ORDER BY untuk hasil yang konsisten
	// Default sort by upload time descending (file terbaru duluan)
	query := `
		SELECT id, filename, content_type, size, object_key, description, uploaded_at, uploaded_by
		FROM files
		ORDER BY uploaded_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}
	defer rows.Close()

	// Gunakan slice dengan kapasitas awal untuk efisiensi memory
	files := make([]*FileMetadata, 0, limit)

	// Iterasi semua rows hasil query
	for rows.Next() {
		file := &FileMetadata{}
		err := rows.Scan(
			&file.ID,
			&file.Filename,
			&file.ContentType,
			&file.Size,
			&file.ObjectKey,
			&file.Description,
			&file.UploadedAt,
			&file.UploadedBy,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan file row: %w", err)
		}

		files = append(files, file)
	}

	// Check error yang mungkin terjadi selama iterasi
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating file rows: %w", err)
	}

	return files, nil
}

// Delete menghapus metadata file dari database
// Catatan: Ini hanya menghapus metadata, file di object storage harus dihapus terpisah
func (r *fileRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM files WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete file metadata: %w", err)
	}

	// Cek apakah ada row yang terhapus
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("file not found")
	}

	return nil
}

// Count menghitung total jumlah file di database
// Berguna untuk pagination dan statistik
func (r *fileRepository) Count(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM files`

	var count int
	err := r.db.QueryRowContext(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count files: %w", err)
	}

	return count, nil
}
