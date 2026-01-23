-- Database initialization script untuk RustFS Golang Demo
-- Script ini akan dijalankan otomatis oleh PostgreSQL saat container pertama kali dibuat

-- Buat extension untuk UUID generation jika belum ada
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Tabel files menyimpan metadata dari semua file yang diupload
-- Metadata ini terpisah dari file actual yang disimpan di RustFS object storage
-- Pemisahan ini memberikan beberapa keuntungan:
-- 1. Query cepat tanpa harus akses object storage
-- 2. Bisa menambahkan field custom tanpa ubah storage
-- 3. Mendukung fitur seperti search, filtering, dan analytics
CREATE TABLE IF NOT EXISTS files (
    -- Primary key menggunakan UUID untuk uniqueness dan security
    -- UUID lebih baik dari auto-increment integer karena tidak predictable
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

    -- Nama file original yang diupload oleh user
    -- Disimpan untuk display purposes dan download dengan nama asli
    filename VARCHAR(255) NOT NULL,

    -- MIME type untuk proper HTTP response headers
    -- Contoh: application/pdf, image/jpeg, text/plain
    content_type VARCHAR(100) NOT NULL,

    -- Ukuran file dalam bytes
    -- Berguna untuk validation, quotas, dan display ke user
    size BIGINT NOT NULL CHECK (size >= 0),

    -- Path/key dari object di RustFS storage
    -- Format: files/{year}/{month}/{uuid}_{filename}
    -- Field ini unik dan digunakan untuk akses file di storage
    object_key VARCHAR(500) NOT NULL UNIQUE,

    -- Deskripsi opsional dari user tentang file
    -- Bisa digunakan untuk search atau categorization
    description TEXT,

    -- Timestamp kapan file diupload
    -- Menggunakan TIMESTAMP WITH TIME ZONE untuk handling timezone
    uploaded_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- User ID yang mengupload file
    -- Saat ini diisi dengan 'system', tapi bisa diganti dengan actual user ID
    -- setelah implementasi authentication
    uploaded_by VARCHAR(100) NOT NULL DEFAULT 'system'
);

-- Index pada uploaded_at untuk sorting dan filtering berdasarkan tanggal
-- Query "get recent files" akan sangat cepat dengan index ini
CREATE INDEX IF NOT EXISTS idx_files_uploaded_at ON files(uploaded_at DESC);

-- Index pada uploaded_by untuk filtering berdasarkan user
-- Berguna ketika implementasi multi-user dan per-user file listing
CREATE INDEX IF NOT EXISTS idx_files_uploaded_by ON files(uploaded_by);

-- Index pada object_key untuk lookup cepat saat akses dari storage
-- Meskipun object_key sudah UNIQUE (yang otomatis create index),
-- explicit index ini untuk dokumentasi
CREATE INDEX IF NOT EXISTS idx_files_object_key ON files(object_key);

-- Tabel untuk tracking file operations (audit trail)
-- Table ini opsional tapi sangat berguna untuk compliance dan debugging
CREATE TABLE IF NOT EXISTS file_operations (
    id SERIAL PRIMARY KEY,
    file_id UUID REFERENCES files(id) ON DELETE SET NULL,
    operation VARCHAR(20) NOT NULL, -- 'upload', 'download', 'delete'
    performed_by VARCHAR(100) NOT NULL,
    performed_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ip_address INET, -- IP address user yang melakukan operasi
    user_agent TEXT, -- Browser/client user agent
    additional_info JSONB -- Informasi tambahan dalam format JSON
);

-- Index untuk audit trail queries
CREATE INDEX IF NOT EXISTS idx_file_operations_file_id ON file_operations(file_id);
CREATE INDEX IF NOT EXISTS idx_file_operations_performed_at ON file_operations(performed_at DESC);

-- Trigger function untuk otomatis log operation saat file dihapus
CREATE OR REPLACE FUNCTION log_file_deletion()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO file_operations (file_id, operation, performed_by, additional_info)
    VALUES (
        OLD.id,
        'delete',
        'system',
        jsonb_build_object(
            'filename', OLD.filename,
            'object_key', OLD.object_key,
            'size', OLD.size
        )
    );
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

-- Attach trigger ke table files
CREATE TRIGGER trigger_log_file_deletion
    BEFORE DELETE ON files
    FOR EACH ROW
    EXECUTE FUNCTION log_file_deletion();

-- View untuk statistics dan monitoring
-- View ini mempermudah query untuk dashboard atau monitoring
CREATE OR REPLACE VIEW file_statistics AS
SELECT
    COUNT(*) as total_files,
    SUM(size) as total_size_bytes,
    ROUND(AVG(size)) as avg_file_size_bytes,
    MAX(size) as largest_file_bytes,
    MIN(size) as smallest_file_bytes,
    COUNT(DISTINCT uploaded_by) as unique_uploaders,
    MAX(uploaded_at) as last_upload_time
FROM files;

-- View untuk daily upload statistics
CREATE OR REPLACE VIEW daily_upload_stats AS
SELECT
    DATE(uploaded_at) as upload_date,
    COUNT(*) as files_uploaded,
    SUM(size) as total_size_bytes,
    COUNT(DISTINCT uploaded_by) as unique_uploaders
FROM files
GROUP BY DATE(uploaded_at)
ORDER BY upload_date DESC;

-- Insert sample data untuk testing (opsional, bisa dihapus di production)
-- Uncomment baris di bawah jika ingin sample data
-- INSERT INTO files (filename, content_type, size, object_key, description)
-- VALUES
--     ('sample-document.pdf', 'application/pdf', 1024000, 'files/2025/01/sample-doc.pdf', 'Sample PDF document'),
--     ('image.jpg', 'image/jpeg', 512000, 'files/2025/01/sample-img.jpg', 'Sample image file');

-- Grant permissions (jika diperlukan untuk multiple database users)
-- GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO rustfsuser;
-- GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO rustfsuser;

-- Commit transaction
COMMIT;
