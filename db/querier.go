// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.29.0

package db

import (
	"context"
	"database/sql"
)

type Querier interface {
	CreateProject(ctx context.Context, arg CreateProjectParams) error
	CreateRecord(ctx context.Context, arg CreateRecordParams) error
	CreateRecordTag(ctx context.Context, arg CreateRecordTagParams) error
	DeleteProject(ctx context.Context, project string) error
	DeleteProjectEntity(ctx context.Context, name string) error
	DeleteRecord(ctx context.Context, id string) (sql.Result, error)
	DeleteRecordTags(ctx context.Context, recordID string) error
	DeleteRecordsUntil(ctx context.Context, timestamp string) (sql.Result, error)
	DeleteRecordsUntilByProject(ctx context.Context, arg DeleteRecordsUntilByProjectParams) (sql.Result, error)
	GetProject(ctx context.Context, name string) (Project, error)
	GetProjectTags(ctx context.Context, project string) ([]string, error)
	GetRecord(ctx context.Context, id string) (Record, error)
	GetRecordTags(ctx context.Context, recordID string) ([]string, error)
	ListProjects(ctx context.Context) ([]Project, error)
	// Note: BETWEEN clause must come first due to sqlc bug with SQLite parameter handling
	ListRecords(ctx context.Context, arg ListRecordsParams) ([]Record, error)
	// Note: BETWEEN clause must come first due to sqlc bug with SQLite parameter handling
	// Returns records that have any of the specified tags
	ListRecordsWithTags(ctx context.Context, arg ListRecordsWithTagsParams) ([]Record, error)
	UpdateProject(ctx context.Context, arg UpdateProjectParams) (sql.Result, error)
	UpdateRecord(ctx context.Context, arg UpdateRecordParams) (sql.Result, error)
}

var _ Querier = (*Queries)(nil)
