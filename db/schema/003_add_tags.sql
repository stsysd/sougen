-- Create separate tags table for better normalization and query performance
CREATE TABLE tags (
    record_id TEXT NOT NULL,
    tag TEXT NOT NULL,
    PRIMARY KEY (record_id, tag),
    FOREIGN KEY (record_id) REFERENCES records(id) ON DELETE CASCADE
);

-- Create indexes for efficient tag queries
CREATE INDEX idx_tags_record_id ON tags(record_id);
CREATE INDEX idx_tags_tag ON tags(tag);