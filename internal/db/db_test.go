package db

import (
	"testing"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite" // SQLite driver for testing
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) {
	var err error
	DB, err = gorm.Open("sqlite3", ":memory:")
	require.NoError(t, err, "Failed to create test database")

	// Migrate the schema
	err = DB.AutoMigrate(&Link{}).Error
	require.NoError(t, err, "Failed to migrate test database")
}

func teardownTestDB(t *testing.T) {
	if DB != nil {
		err := DB.Close()
		assert.NoError(t, err, "Failed to close test database")
	}
}

func TestInitDB(t *testing.T) {
	// Skip this test since InitDB is hardcoded for postgres
	// We test the database operations with in-memory SQLite in other tests
	t.Skip("InitDB is hardcoded for postgres, skipping in tests")
}

func TestCreateLink(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	tests := []struct {
		name    string
		link    *Link
		wantErr bool
	}{
		{
			name: "valid link",
			link: &Link{
				ShortCode:    "ABC123",
				OriginalURL:  "https://example.com",
				RenderStatus: RenderStatusPending,
			},
			wantErr: false,
		},
		{
			name: "duplicate short code",
			link: &Link{
				ShortCode:    "ABC123", // Same as above
				OriginalURL:  "https://different.com",
				RenderStatus: RenderStatusPending,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CreateLink(tt.link)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotZero(t, tt.link.ID)
			}
		})
	}
}

func TestGetLinkByShortCode(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	// Create test data
	testLink := &Link{
		ShortCode:           "TEST123",
		OriginalURL:         "https://test.com",
		RenderedHTMLContent: "<html>Test</html>",
		RenderStatus:        RenderStatusCompleted,
	}
	err := CreateLink(testLink)
	require.NoError(t, err)

	tests := []struct {
		name      string
		shortCode string
		wantErr   bool
		wantLink  *Link
	}{
		{
			name:      "existing short code",
			shortCode: "TEST123",
			wantErr:   false,
			wantLink:  testLink,
		},
		{
			name:      "non-existing short code",
			shortCode: "NOTFOUND",
			wantErr:   true,
			wantLink:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link, err := GetLinkByShortCode(tt.shortCode)
			if tt.wantErr {
				assert.Error(t, err)
				assert.True(t, gorm.IsRecordNotFoundError(err))
				assert.Nil(t, link)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, link)
				assert.Equal(t, tt.wantLink.ShortCode, link.ShortCode)
				assert.Equal(t, tt.wantLink.OriginalURL, link.OriginalURL)
				assert.Equal(t, tt.wantLink.RenderStatus, link.RenderStatus)
			}
		})
	}
}

func TestGetLinkByOriginalURL(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	// Create test data
	testLink := &Link{
		ShortCode:    "TEST456",
		OriginalURL:  "https://original.com",
		RenderStatus: RenderStatusPending,
	}
	err := CreateLink(testLink)
	require.NoError(t, err)

	tests := []struct {
		name        string
		originalURL string
		wantErr     bool
		wantLink    *Link
	}{
		{
			name:        "existing original URL",
			originalURL: "https://original.com",
			wantErr:     false,
			wantLink:    testLink,
		},
		{
			name:        "non-existing original URL",
			originalURL: "https://notfound.com",
			wantErr:     true,
			wantLink:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link, err := GetLinkByOriginalURL(tt.originalURL)
			if tt.wantErr {
				assert.Error(t, err)
				assert.True(t, gorm.IsRecordNotFoundError(err))
				assert.Nil(t, link)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, link)
				assert.Equal(t, tt.wantLink.ShortCode, link.ShortCode)
				assert.Equal(t, tt.wantLink.OriginalURL, link.OriginalURL)
			}
		})
	}
}

func TestUpdateLinkRenderStatus(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	// Create test data
	testLink := &Link{
		ShortCode:    "UPDATE123",
		OriginalURL:  "https://update.com",
		RenderStatus: RenderStatusPending,
	}
	err := CreateLink(testLink)
	require.NoError(t, err)

	tests := []struct {
		name      string
		shortCode string
		status    RenderStatus
		wantErr   bool
	}{
		{
			name:      "update to rendering",
			shortCode: "UPDATE123",
			status:    RenderStatusRendering,
			wantErr:   false,
		},
		{
			name:      "update to completed",
			shortCode: "UPDATE123",
			status:    RenderStatusCompleted,
			wantErr:   false,
		},
		{
			name:      "update non-existing",
			shortCode: "NOTFOUND",
			status:    RenderStatusCompleted,
			wantErr:   false, // GORM doesn't return error for 0 rows affected
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := UpdateLinkRenderStatus(tt.shortCode, tt.status)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)

				// Verify the update if it's a valid short code
				if tt.shortCode == "UPDATE123" {
					link, err := GetLinkByShortCode(tt.shortCode)
					assert.NoError(t, err)
					assert.Equal(t, tt.status, link.RenderStatus)
				}
			}
		})
	}
}

func TestUpdateLinkContent(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	// Create test data
	testLink := &Link{
		ShortCode:    "CONTENT123",
		OriginalURL:  "https://content.com",
		RenderStatus: RenderStatusRendering,
	}
	err := CreateLink(testLink)
	require.NoError(t, err)

	tests := []struct {
		name        string
		shortCode   string
		htmlContent string
		status      RenderStatus
		wantErr     bool
	}{
		{
			name:        "update with content",
			shortCode:   "CONTENT123",
			htmlContent: "<html><body>Test Content</body></html>",
			status:      RenderStatusCompleted,
			wantErr:     false,
		},
		{
			name:        "update with failure",
			shortCode:   "CONTENT123",
			htmlContent: "",
			status:      RenderStatusFailed,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := UpdateLinkContent(tt.shortCode, tt.htmlContent, tt.status)
			assert.NoError(t, err)

			// Verify the update
			link, err := GetLinkByShortCode(tt.shortCode)
			assert.NoError(t, err)
			assert.Equal(t, tt.htmlContent, link.RenderedHTMLContent)
			assert.Equal(t, tt.status, link.RenderStatus)
		})
	}
}

func TestRenderStatus(t *testing.T) {
	tests := []struct {
		name   string
		status RenderStatus
	}{
		{"pending", RenderStatusPending},
		{"rendering", RenderStatusRendering},
		{"completed", RenderStatusCompleted},
		{"failed", RenderStatusFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotEmpty(t, string(tt.status))
		})
	}
}

func TestLinkModel(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	link := &Link{
		ShortCode:           "MODEL123",
		OriginalURL:         "https://model.test",
		RenderedHTMLContent: "<html>Model Test</html>",
		RenderStatus:        RenderStatusCompleted,
	}

	// Test creation
	err := CreateLink(link)
	assert.NoError(t, err)
	assert.NotZero(t, link.ID)
	assert.NotZero(t, link.CreatedAt)
	assert.NotZero(t, link.UpdatedAt)

	// Test retrieval
	retrieved, err := GetLinkByShortCode("MODEL123")
	assert.NoError(t, err)
	assert.Equal(t, link.ShortCode, retrieved.ShortCode)
	assert.Equal(t, link.OriginalURL, retrieved.OriginalURL)
	assert.Equal(t, link.RenderedHTMLContent, retrieved.RenderedHTMLContent)
	assert.Equal(t, link.RenderStatus, retrieved.RenderStatus)
}

func TestResetStuckRenderingTasks(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	// Create test data with various render statuses
	testLinks := []Link{
		{
			ShortCode:    "STUCK1",
			OriginalURL:  "https://stuck1.com",
			RenderStatus: RenderStatusRendering, // This should be reset
		},
		{
			ShortCode:    "STUCK2",
			OriginalURL:  "https://stuck2.com",
			RenderStatus: RenderStatusRendering, // This should be reset
		},
		{
			ShortCode:    "COMPLETE1",
			OriginalURL:  "https://complete1.com",
			RenderStatus: RenderStatusCompleted, // This should not be affected
		},
		{
			ShortCode:    "PENDING1",
			OriginalURL:  "https://pending1.com",
			RenderStatus: RenderStatusPending, // This should not be affected
		},
		{
			ShortCode:    "FAILED1",
			OriginalURL:  "https://failed1.com",
			RenderStatus: RenderStatusFailed, // This should not be affected
		},
	}

	// Insert test data
	for _, link := range testLinks {
		err := CreateLink(&link)
		require.NoError(t, err)
	}

	// Test resetting stuck tasks
	affected, err := ResetStuckRenderingTasks()
	require.NoError(t, err)
	assert.Equal(t, int64(2), affected, "Should have reset exactly 2 stuck tasks")

	// Verify the results
	for _, link := range testLinks {
		updatedLink, err := GetLinkByShortCode(link.ShortCode)
		require.NoError(t, err)

		switch link.ShortCode {
		case "STUCK1", "STUCK2":
			// These should have been reset to pending
			assert.Equal(t, RenderStatusPending, updatedLink.RenderStatus,
				"Stuck task %s should have been reset to pending", link.ShortCode)
		default:
			// These should remain unchanged
			assert.Equal(t, link.RenderStatus, updatedLink.RenderStatus,
				"Non-stuck task %s should have remained unchanged", link.ShortCode)
		}
	}

	// Test resetting when no stuck tasks exist
	affected, err = ResetStuckRenderingTasks()
	require.NoError(t, err)
	assert.Equal(t, int64(0), affected, "Should have found no stuck tasks to reset")

	// Test with empty database
	teardownTestDB(t)
	setupTestDB(t)
	affected, err = ResetStuckRenderingTasks()
	require.NoError(t, err)
	assert.Equal(t, int64(0), affected, "Should handle empty database gracefully")
}
