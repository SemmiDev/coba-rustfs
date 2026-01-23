package main

import (
	"os"
	"strconv"
)

// Config menyimpan semua konfigurasi aplikasi yang dibaca dari environment variables
type Config struct {
	App      AppConfig
	Database DatabaseConfig
	RustFS   RustFSConfig
}

// AppConfig menyimpan konfigurasi umum aplikasi
type AppConfig struct {
	Port     string
	Env      string
	LogLevel string
}

// DatabaseConfig menyimpan konfigurasi koneksi database PostgreSQL
type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
	SSLMode  string
}

// RustFSConfig menyimpan konfigurasi untuk koneksi ke RustFS object storage
// RustFS kompatibel dengan S3 API, jadi konfigurasi mirip dengan AWS S3
type RustFSConfig struct {
	Endpoint  string // URL endpoint RustFS (contoh: http://localhost:9000)
	AccessKey string // Access key untuk autentikasi (seperti AWS Access Key ID)
	SecretKey string // Secret key untuk autentikasi (seperti AWS Secret Access Key)
	Bucket    string // Nama default bucket untuk menyimpan file
	Region    string // Region (untuk kompatibilitas S3, biasanya us-east-1)
	UseSSL    bool   // Apakah menggunakan HTTPS atau HTTP
}

// Load membaca semua environment variables dan mengembalikan struct Config
// Fungsi ini menggunakan nilai default jika environment variable tidak diset
func Load() *Config {
	return &Config{
		App: AppConfig{
			Port:     getEnv("APP_PORT", "8080"),
			Env:      getEnv("APP_ENV", "development"),
			LogLevel: getEnv("LOG_LEVEL", "info"),
		},
		Database: DatabaseConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnv("DB_PORT", "5432"),
			User:     getEnv("DB_USER", "rustfsuser"),
			Password: getEnv("DB_PASSWORD", "rustfspass"),
			Name:     getEnv("DB_NAME", "rustfsdb"),
			SSLMode:  getEnv("DB_SSLMODE", "disable"),
		},
		RustFS: RustFSConfig{
			Endpoint:  getEnv("RUSTFS_ENDPOINT", "http://localhost:9000"),
			AccessKey: getEnv("RUSTFS_ACCESS_KEY", "rustfsadmin"),
			SecretKey: getEnv("RUSTFS_SECRET_KEY", "rustfsadmin"),
			Bucket:    getEnv("RUSTFS_BUCKET", "demo-bucket"),
			Region:    getEnv("RUSTFS_REGION", "us-east-1"),
			UseSSL:    getEnvAsBool("RUSTFS_USE_SSL", false),
		},
	}
}

// getEnv mengambil nilai environment variable atau mengembalikan nilai default
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvAsBool mengambil nilai environment variable sebagai boolean
// String "true", "1", "yes" akan dianggap true, selainnya false
func getEnvAsBool(key string, defaultValue bool) bool {
	valStr := getEnv(key, "")
	if valStr == "" {
		return defaultValue
	}

	val, err := strconv.ParseBool(valStr)
	if err != nil {
		return defaultValue
	}

	return val
}
