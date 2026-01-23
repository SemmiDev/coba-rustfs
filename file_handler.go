package main

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// FileHandler menangani semua HTTP requests terkait file management
// Handler ini bertindak sebagai adapter antara HTTP layer dan business logic layer
type FileHandler struct {
	storageService StorageService
}

// NewFileHandler membuat instance baru FileHandler dengan dependency injection
func NewFileHandler(storageService StorageService) *FileHandler {
	return &FileHandler{
		storageService: storageService,
	}
}

// UploadFile menangani HTTP request untuk upload file
// Method: POST
// Path: /api/files
// Request: multipart/form-data dengan field "file" dan optional "description"
// Response: JSON dengan metadata file yang diupload
func (h *FileHandler) UploadFile(c *gin.Context) {
	// Step 1: Parse multipart form dengan size limit
	// MaxMultipartMemory sudah diset di router (100MB)
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Failed to get file from request",
			"message": err.Error(),
		})
		return
	}

	// Step 2: Get optional description dari form
	description := c.PostForm("description")

	// Step 3: Call service layer untuk process upload
	// Service layer akan handle business logic dan koordinasi antara storage dan database
	metadata, err := h.storageService.UploadFile(c.Request.Context(), file, description)
	if err != nil {
		// Error bisa dari berbagai sumber: validasi, storage, database
		// Status code disesuaikan dengan jenis error
		statusCode := http.StatusInternalServerError
		if err.Error() == "file size exceeds maximum limit of 100MB" {
			statusCode = http.StatusBadRequest
		}

		c.JSON(statusCode, gin.H{
			"error":   "Failed to upload file",
			"message": err.Error(),
		})
		return
	}

	// Step 4: Return success response dengan metadata file
	c.JSON(http.StatusCreated, gin.H{
		"message": "File uploaded successfully",
		"data":    metadata,
	})
}

// ListFiles menangani HTTP request untuk mendapatkan daftar file dengan pagination
// Method: GET
// Path: /api/files
// Query params: limit (default: 10), offset (default: 0)
// Response: JSON dengan array files dan pagination metadata
func (h *FileHandler) ListFiles(c *gin.Context) {
	// Step 1: Parse pagination parameters dari query string
	// Gunakan default values jika parameter tidak ada
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	// Step 2: Get list files dari service layer
	files, totalCount, err := h.storageService.ListFiles(c.Request.Context(), limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to list files",
			"message": err.Error(),
		})
		return
	}

	// Step 3: Calculate pagination metadata untuk membantu client
	// Informasi ini berguna untuk UI pagination component
	totalPages := (totalCount + limit - 1) / limit
	currentPage := (offset / limit) + 1

	// Step 4: Return response dengan data dan pagination info
	c.JSON(http.StatusOK, gin.H{
		"data": files,
		"pagination": gin.H{
			"total":        totalCount,
			"limit":        limit,
			"offset":       offset,
			"total_pages":  totalPages,
			"current_page": currentPage,
		},
	})
}

// GetFile menangani HTTP request untuk mendapatkan metadata file tertentu
// Method: GET
// Path: /api/files/:id
// Response: JSON dengan metadata file
func (h *FileHandler) GetFile(c *gin.Context) {
	// Step 1: Extract file ID dari URL parameter
	fileID := c.Param("id")
	if fileID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "File ID is required",
		})
		return
	}

	// Step 2: Get file metadata dari service layer
	metadata, err := h.storageService.GetFile(c.Request.Context(), fileID)
	if err != nil {
		// Distinguish antara "not found" dan "server error"
		statusCode := http.StatusInternalServerError
		if err.Error() == "file not found" {
			statusCode = http.StatusNotFound
		}

		c.JSON(statusCode, gin.H{
			"error":   "Failed to get file",
			"message": err.Error(),
		})
		return
	}

	// Step 3: Return metadata
	c.JSON(http.StatusOK, gin.H{
		"data": metadata,
	})
}

// DownloadFile menangani HTTP request untuk download file
// Method: GET
// Path: /api/files/:id/download
// Response: File binary dengan proper headers untuk browser download
func (h *FileHandler) DownloadFile(c *gin.Context) {
	// Step 1: Extract file ID dari URL parameter
	fileID := c.Param("id")
	if fileID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "File ID is required",
		})
		return
	}

	// Step 2: Download file dari storage melalui service layer
	// Service akan return ReadCloser (stream) untuk efisiensi memory
	fileStream, metadata, err := h.storageService.DownloadFile(c.Request.Context(), fileID)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if err.Error() == "file not found" {
			statusCode = http.StatusNotFound
		}

		c.JSON(statusCode, gin.H{
			"error":   "Failed to download file",
			"message": err.Error(),
		})
		return
	}
	defer fileStream.Close()

	// Step 3: Set response headers untuk trigger browser download
	// Content-Disposition: attachment membuat browser download file bukan display
	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Transfer-Encoding", "binary")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, metadata.Filename))
	c.Header("Content-Type", metadata.ContentType)
	c.Header("Content-Length", fmt.Sprintf("%d", metadata.Size))

	// Step 4: Stream file ke response
	// io.Copy akan efficiently stream dari source ke destination tanpa load semua ke memory
	_, err = io.Copy(c.Writer, fileStream)
	if err != nil {
		// Jika error terjadi saat streaming, response sudah partially sent
		// Kita tidak bisa return JSON error lagi, hanya log error
		c.Error(fmt.Errorf("failed to stream file: %w", err))
		return
	}
}

// DeleteFile menangani HTTP request untuk menghapus file
// Method: DELETE
// Path: /api/files/:id
// Response: JSON success message
func (h *FileHandler) DeleteFile(c *gin.Context) {
	// Step 1: Extract file ID dari URL parameter
	fileID := c.Param("id")
	if fileID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "File ID is required",
		})
		return
	}

	// Step 2: Delete file melalui service layer
	// Service akan handle deletion dari storage dan database
	err := h.storageService.DeleteFile(c.Request.Context(), fileID)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if err.Error() == "file not found" {
			statusCode = http.StatusNotFound
		}

		c.JSON(statusCode, gin.H{
			"error":   "Failed to delete file",
			"message": err.Error(),
		})
		return
	}

	// Step 3: Return success response
	// HTTP 204 No Content adalah standard untuk successful DELETE
	// Tapi kita gunakan 200 dengan message untuk user feedback yang lebih baik
	c.JSON(http.StatusOK, gin.H{
		"message": "File deleted successfully",
	})
}

// GetPresignedURL menangani HTTP request untuk generate pre-signed URL
// Method: GET
// Path: /api/files/:id/presigned-url
// Query params: expiry (optional, dalam detik, default: 3600 = 1 jam)
// Response: JSON dengan pre-signed URL
//
// Pre-signed URL berguna untuk:
// - Sharing file dengan pihak eksternal tanpa expose credentials
// - Direct download dari storage tanpa melewati aplikasi server (menghemat bandwidth)
// - Temporary access dengan automatic expiry
func (h *FileHandler) GetPresignedURL(c *gin.Context) {
	// Step 1: Extract file ID dari URL parameter
	fileID := c.Param("id")
	if fileID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "File ID is required",
		})
		return
	}

	// Step 2: Parse expiry duration dari query parameter
	// Default: 1 jam (3600 detik)
	// Maximum: 7 hari untuk security reasons
	expirySeconds, _ := strconv.Atoi(c.DefaultQuery("expiry", "3600"))
	maxExpiry := 7 * 24 * 60 * 60 // 7 hari dalam detik

	if expirySeconds <= 0 || expirySeconds > maxExpiry {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid expiry duration",
			"message": fmt.Sprintf("Expiry must be between 1 and %d seconds", maxExpiry),
		})
		return
	}

	expiry := time.Duration(expirySeconds) * time.Second

	// Step 3: Generate pre-signed URL melalui service layer
	url, err := h.storageService.GeneratePresignedURL(c.Request.Context(), fileID, expiry)
	if err != nil {
		statusCode := http.StatusInternalServerError
		if err.Error() == "file not found" {
			statusCode = http.StatusNotFound
		}

		c.JSON(statusCode, gin.H{
			"error":   "Failed to generate presigned URL",
			"message": err.Error(),
		})
		return
	}

	// Step 4: Return pre-signed URL dengan informasi expiry
	c.JSON(http.StatusOK, gin.H{
		"url":        url,
		"expires_in": expirySeconds,
		"expires_at": time.Now().Add(expiry).Format(time.RFC3339),
	})
}
