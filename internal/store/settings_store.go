package store

import (
	"context"
	"database/sql"
)

const ModelTimeoutSettingKey = "model_timeout_seconds"
const LocalModelEndpointSettingKey = "local_model_endpoint"
const LocalModelIDSettingKey = "local_model_id"
const VisionModelEndpointSettingKey = "vision_model_endpoint"
const VisionModelIDSettingKey = "vision_model_id"

// SettingsStore persists non-secret application preferences.
type SettingsStore struct{ db *sql.DB }

func (s *SettingsStore) Get(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func (s *SettingsStore) Set(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO app_settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	return err
}

func (s *SettingsStore) Delete(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM app_settings WHERE key = ?`, key)
	return err
}
