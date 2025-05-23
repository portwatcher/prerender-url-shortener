package db

import (
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres" // PostgreSQL driver
)

// RenderStatus represents the rendering status of a URL
type RenderStatus string

const (
	RenderStatusPending   RenderStatus = "pending"
	RenderStatusRendering RenderStatus = "rendering"
	RenderStatusCompleted RenderStatus = "completed"
	RenderStatusFailed    RenderStatus = "failed"
)

// Link represents the data model for a shortened URL.
type Link struct {
	gorm.Model
	ShortCode           string       `gorm:"unique_index;not null"`
	OriginalURL         string       `gorm:"not null;index"`
	RenderedHTMLContent string       `gorm:"type:text"` // Use text for potentially large HTML
	RenderStatus        RenderStatus `gorm:"type:varchar(20);default:'pending';not null"`
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

// GetLinkByOriginalURL retrieves a link by its original URL.
func GetLinkByOriginalURL(originalURL string) (*Link, error) {
	var link Link
	if err := DB.Where("original_url = ?", originalURL).First(&link).Error; err != nil {
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

// UpdateLinkRenderStatus updates the render status of a link.
func UpdateLinkRenderStatus(shortCode string, status RenderStatus) error {
	return DB.Model(&Link{}).Where("short_code = ?", shortCode).Update("render_status", status).Error
}

// UpdateLinkContent updates the rendered HTML content and status of a link.
func UpdateLinkContent(shortCode string, htmlContent string, status RenderStatus) error {
	return DB.Model(&Link{}).Where("short_code = ?", shortCode).Updates(map[string]interface{}{
		"rendered_html_content": htmlContent,
		"render_status":         status,
	}).Error
}
