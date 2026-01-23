package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
)

func main() {
	// Load konfigurasi dari environment variables
	cfg := Load()

	// Setup database connection dengan retry mechanism
	// Hal ini penting karena aplikasi mungkin start lebih dulu sebelum database ready
	db, err := setupDatabase(cfg)
	if err != nil {
		log.Fatalf("Failed to setup database: %v", err)
	}
	defer db.Close()

	log.Println("Database connection established successfully")

	// Setup S3 client untuk berkomunikasi dengan RustFS
	// RustFS kompatibel dengan S3 API, jadi kita gunakan AWS SDK
	s3Client := setupS3Client(cfg)

	// Buat bucket jika belum ada
	// Bucket adalah container untuk menyimpan object/file di object storage
	if err := ensureBucketExists(s3Client, cfg.RustFS.Bucket); err != nil {
		log.Printf("Warning: Failed to ensure bucket exists: %v", err)
	} else {
		log.Printf("Bucket '%s' is ready", cfg.RustFS.Bucket)
	}

	// Initialize repository layer (data access)
	fileRepo := NewFileRepository(db)

	// Initialize service layer (business logic)
	storageService := NewStorageService(s3Client, fileRepo, cfg)

	// Initialize handler layer (HTTP handlers)
	fileHandler := NewFileHandler(storageService)

	// Setup HTTP router menggunakan Gin framework
	router := setupRouter(fileHandler)

	// Setup graceful shutdown
	// Ini memungkinkan aplikasi untuk shutdown dengan baik ketika menerima signal
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%s", cfg.App.Port),
		Handler: router,
	}

	// Jalankan server di goroutine terpisah
	go func() {
		log.Printf("Starting server on port %s", cfg.App.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait untuk interrupt signal untuk graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// Timeout context untuk shutdown - beri waktu 5 detik
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	log.Println("Server exited properly")
}

// setupDatabase membuat koneksi ke PostgreSQL dengan retry mechanism
func setupDatabase(cfg *Config) (*sql.DB, error) {
	// Connection string untuk PostgreSQL
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		cfg.Database.Host,
		cfg.Database.Port,
		cfg.Database.User,
		cfg.Database.Password,
		cfg.Database.Name,
		cfg.Database.SSLMode,
	)

	var db *sql.DB
	var err error

	// Retry connection sampai 10 kali dengan delay 2 detik
	// Ini penting karena PostgreSQL mungkin belum siap saat aplikasi start
	for i := 0; i < 10; i++ {
		db, err = sql.Open("postgres", dsn)
		if err != nil {
			log.Printf("Failed to open database connection (attempt %d/10): %v", i+1, err)
			time.Sleep(2 * time.Second)
			continue
		}

		// Test koneksi
		err = db.Ping()
		if err == nil {
			break
		}

		log.Printf("Failed to ping database (attempt %d/10): %v", i+1, err)
		time.Sleep(2 * time.Second)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect to database after 10 attempts: %w", err)
	}

	// Set connection pool settings untuk performa optimal
	db.SetMaxOpenConns(25)                 // Maksimal 25 koneksi concurrent
	db.SetMaxIdleConns(5)                  // Maksimal 5 koneksi idle
	db.SetConnMaxLifetime(5 * time.Minute) // Recycle koneksi setiap 5 menit

	return db, nil
}

// setupS3Client membuat S3 client untuk berkomunikasi dengan RustFS
func setupS3Client(cfg *Config) *s3.Client {
	// Custom resolver untuk endpoint RustFS
	// Ini diperlukan karena kita tidak menggunakan AWS S3 asli
	customResolver := aws.EndpointResolverWithOptionsFunc(
		func(service, region string, options ...interface{}) (aws.Endpoint, error) {
			if service == s3.ServiceID {
				return aws.Endpoint{
					URL:               cfg.RustFS.Endpoint,
					SigningRegion:     cfg.RustFS.Region,
					HostnameImmutable: true, // Jangan ubah hostname
				}, nil
			}
			return aws.Endpoint{}, &aws.EndpointNotFoundError{}
		},
	)

	// Buat AWS config dengan static credentials
	awsConfig := aws.Config{
		Region: cfg.RustFS.Region,
		Credentials: credentials.NewStaticCredentialsProvider(
			cfg.RustFS.AccessKey,
			cfg.RustFS.SecretKey,
			"", // Session token kosong
		),
		EndpointResolverWithOptions: customResolver,
	}

	// Buat S3 client
	client := s3.NewFromConfig(awsConfig, func(o *s3.Options) {
		// Force path style untuk kompatibilitas dengan RustFS
		// Path style: http://endpoint/bucket/key
		// Virtual hosted style: http://bucket.endpoint/key
		o.UsePathStyle = true
	})

	return client
}

// ensureBucketExists memastikan bucket sudah ada, jika belum akan dibuat
func ensureBucketExists(client *s3.Client, bucketName string) error {
	ctx := context.Background()

	// Cek apakah bucket sudah ada
	_, err := client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
	})

	if err == nil {
		// Bucket sudah ada
		return nil
	}

	// Bucket belum ada, buat bucket baru
	log.Printf("Bucket '%s' does not exist, creating...", bucketName)

	_, err = client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	})

	if err != nil {
		return fmt.Errorf("failed to create bucket: %w", err)
	}

	log.Printf("Bucket '%s' created successfully", bucketName)
	return nil
}

// setupRouter mengonfigurasi routing dan middleware
func setupRouter(fileHandler *FileHandler) *gin.Engine {
	// Set Gin mode berdasarkan environment
	if os.Getenv("APP_ENV") == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.Default()

	// CORS middleware untuk mengizinkan akses dari frontend
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// Limit request size untuk upload (maksimal 100MB)
	router.MaxMultipartMemory = 100 << 20 // 100 MB

	// Health check endpoint
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "healthy",
			"service": "rustfs-golang-demo",
			"time":    time.Now().Format(time.RFC3339),
		})
	})

	// Serve static files (web UI)
	router.StaticFile("/", "./web/index.html")
	router.Static("/static", "./web/static")

	// API routes group
	api := router.Group("/api")
	{
		// File management endpoints
		api.POST("/files", fileHandler.UploadFile)                       // Upload file
		api.GET("/files", fileHandler.ListFiles)                         // List semua files
		api.GET("/files/:id", fileHandler.GetFile)                       // Get file metadata
		api.GET("/files/:id/download", fileHandler.DownloadFile)         // Download file
		api.DELETE("/files/:id", fileHandler.DeleteFile)                 // Delete file
		api.GET("/files/:id/presigned-url", fileHandler.GetPresignedURL) // Generate presigned URL
	}

	return router
}
