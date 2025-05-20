package db

import (
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres" // PostgreSQL driver
)

// Link represents the data model for a shortened URL.
type Link struct {
	gorm.Model
	ShortCode           string `gorm:"unique_index;not null"`
	OriginalURL         string `gorm:"not null"`
	RenderedHTMLContent string `gorm:"type:text"` // Use text for potentially large HTML
}

var DB *gorm.DB

// InitDB initializes the database connection and migrates the schema.
func InitDB(dataSourceName string) error {
	var err error
	DB, err = gorm.Open("postgres", dataSourceName)
	if err != nil {
		return err
	}

	// Migrate the schema
	DB.AutoMigrate(&Link{})

	return nil
}

// GetLinkByShortCode retrieves a link by its short code.
func GetLinkByShortCode(shortCode string) (*Link, error) {
	var link Link
	if err := DB.Where("short_code = ?", shortCode).First(&link).Error; err != nil {
		return nil, err
	}
	return &link, nil
}

// CreateLink creates a new link record in the database.
func CreateLink(link *Link) error {
	if err := DB.Create(link).Error; err != nil {
		return err
	}
	return nil
}
