package repository

import "database/sql"

type ScannerRepository interface {
	Scan() error
	GetTrack() (string, error)
}

type SQLiteScannerRepository struct {
	db *sql.DB
}

var _ ScannerRepository = (*SQLiteScannerRepository)(nil)

func NewScannerRepository(db *sql.DB) ScannerRepository {
	return &SQLiteScannerRepository{db: db}
}

func (r *SQLiteScannerRepository) Scan() error {
	return nil
}

func (r *SQLiteScannerRepository) GetTrack() (string, error) {
	return "", nil
}
