package model

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestNewProject tests the NewProject constructor
func TestNewProject(t *testing.T) {
	name := "test-project"
	description := "Test description"

	project, err := NewProject(name, description)
	if err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	// IDフィールドが自動生成されているか確認
	if project.ID == uuid.Nil {
		t.Error("Expected non-nil UUID for ID field")
	}

	// Nameフィールドが正しく設定されているか確認
	if project.Name != name {
		t.Errorf("Expected name %s, got %s", name, project.Name)
	}

	// Descriptionフィールドが正しく設定されているか確認
	if project.Description != description {
		t.Errorf("Expected description %s, got %s", description, project.Description)
	}

	// CreatedAtが設定されているか確認
	if project.CreatedAt.IsZero() {
		t.Error("Expected CreatedAt to be set")
	}

	// UpdatedAtが設定されているか確認
	if project.UpdatedAt.IsZero() {
		t.Error("Expected UpdatedAt to be set")
	}

	// CreatedAtとUpdatedAtが同じ時刻であることを確認（新規作成時）
	if !project.CreatedAt.Equal(project.UpdatedAt) {
		t.Error("Expected CreatedAt and UpdatedAt to be equal for new project")
	}
}

// TestNewProjectEmptyName tests that NewProject fails with empty name
func TestNewProjectEmptyName(t *testing.T) {
	_, err := NewProject("", "Description")
	if err == nil {
		t.Error("Expected error when creating project with empty name, got nil")
	}
}

// TestLoadProject tests the LoadProject constructor
func TestLoadProject(t *testing.T) {
	id := uuid.New()
	name := "loaded-project"
	description := "Loaded description"
	createdAt := testTime()
	updatedAt := testTime().Add(1 * testHour)

	project, err := LoadProject(id, name, description, createdAt, updatedAt)
	if err != nil {
		t.Fatalf("Failed to load project: %v", err)
	}

	// IDフィールドが正しく設定されているか確認
	if project.ID != id {
		t.Errorf("Expected ID %s, got %s", id, project.ID)
	}

	// Nameフィールドが正しく設定されているか確認
	if project.Name != name {
		t.Errorf("Expected name %s, got %s", name, project.Name)
	}

	// Descriptionフィールドが正しく設定されているか確認
	if project.Description != description {
		t.Errorf("Expected description %s, got %s", description, project.Description)
	}

	// CreatedAtが正しく設定されているか確認
	if !project.CreatedAt.Equal(createdAt) {
		t.Errorf("Expected CreatedAt %v, got %v", createdAt, project.CreatedAt)
	}

	// UpdatedAtが正しく設定されているか確認
	if !project.UpdatedAt.Equal(updatedAt) {
		t.Errorf("Expected UpdatedAt %v, got %v", updatedAt, project.UpdatedAt)
	}
}

// TestLoadProjectWithNilID tests that LoadProject fails with nil UUID
func TestLoadProjectWithNilID(t *testing.T) {
	_, err := LoadProject(uuid.Nil, "name", "description", testTime(), testTime())
	if err == nil {
		t.Error("Expected error when loading project with nil UUID, got nil")
	}
}

// TestLoadProjectWithEmptyName tests that LoadProject fails with empty name
func TestLoadProjectWithEmptyName(t *testing.T) {
	_, err := LoadProject(uuid.New(), "", "description", testTime(), testTime())
	if err == nil {
		t.Error("Expected error when loading project with empty name, got nil")
	}
}

// TestLoadProjectWithZeroCreatedAt tests that LoadProject fails with zero CreatedAt
func TestLoadProjectWithZeroCreatedAt(t *testing.T) {
	_, err := LoadProject(uuid.New(), "name", "description", testZeroTime, testTime())
	if err == nil {
		t.Error("Expected error when loading project with zero CreatedAt, got nil")
	}
}

// TestLoadProjectWithZeroUpdatedAt tests that LoadProject fails with zero UpdatedAt
func TestLoadProjectWithZeroUpdatedAt(t *testing.T) {
	_, err := LoadProject(uuid.New(), "name", "description", testTime(), testZeroTime)
	if err == nil {
		t.Error("Expected error when loading project with zero UpdatedAt, got nil")
	}
}

// TestProjectValidate tests the Validate method
func TestProjectValidate(t *testing.T) {
	tests := []struct {
		name        string
		project     *Project
		expectError bool
		description string
	}{
		{
			name: "Valid project",
			project: &Project{
				ID:          uuid.New(),
				Name:        "valid-project",
				Description: "Valid description",
				CreatedAt:   testTime(),
				UpdatedAt:   testTime(),
			},
			expectError: false,
			description: "正常なプロジェクトは検証をパスすること",
		},
		{
			name: "Nil UUID",
			project: &Project{
				ID:          uuid.Nil,
				Name:        "project",
				Description: "Description",
				CreatedAt:   testTime(),
				UpdatedAt:   testTime(),
			},
			expectError: true,
			description: "UUIDがnilの場合はエラーになること",
		},
		{
			name: "Empty name",
			project: &Project{
				ID:          uuid.New(),
				Name:        "",
				Description: "Description",
				CreatedAt:   testTime(),
				UpdatedAt:   testTime(),
			},
			expectError: true,
			description: "名前が空の場合はエラーになること",
		},
		{
			name: "Zero CreatedAt",
			project: &Project{
				ID:          uuid.New(),
				Name:        "project",
				Description: "Description",
				CreatedAt:   testZeroTime,
				UpdatedAt:   testTime(),
			},
			expectError: true,
			description: "CreatedAtがゼロ値の場合はエラーになること",
		},
		{
			name: "Zero UpdatedAt",
			project: &Project{
				ID:          uuid.New(),
				Name:        "project",
				Description: "Description",
				CreatedAt:   testTime(),
				UpdatedAt:   testZeroTime,
			},
			expectError: true,
			description: "UpdatedAtがゼロ値の場合はエラーになること",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.project.Validate()
			if tt.expectError {
				if err == nil {
					t.Errorf("%s: expected error but got nil", tt.description)
				}
			} else {
				if err != nil {
					t.Errorf("%s: unexpected error: %v", tt.description, err)
				}
			}
		})
	}
}
