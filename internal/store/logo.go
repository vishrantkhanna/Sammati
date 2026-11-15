package store

import (
	"context"
	"encoding/base64"
	"net/http"
)

// MaxLogoBytes is the largest logo an organisation can upload.
const MaxLogoBytes = 256 << 10

// logoTypes are the image types accepted for a logo. SVG is refused on
// purpose: an SVG file can carry script.
var logoTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/webp": true}

// SetLogo saves the organisation's logo. The type is read from the bytes
// themselves, not from what the uploader claims.
func (s *Store) SetLogo(ctx context.Context, data []byte) error {
	if len(data) == 0 || len(data) > MaxLogoBytes {
		return invalid("logo", "must be an image of up to 256 KB")
	}
	kind := http.DetectContentType(data)
	if !logoTypes[kind] {
		return invalid("logo", "must be a PNG, JPEG or WebP image")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for k, v := range map[string]string{"logo_type": kind, "logo_data": base64.StdEncoding.EncodeToString(data)} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT (key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Logo returns the organisation's logo and its type, or ErrNotFound.
func (s *Store) Logo(ctx context.Context) (data []byte, kind string, err error) {
	kind, ok, err := s.Setting(ctx, "logo_type")
	if err != nil || !ok {
		return nil, "", firstErr(err, ErrNotFound)
	}
	raw, ok, err := s.Setting(ctx, "logo_data")
	if err != nil || !ok {
		return nil, "", firstErr(err, ErrNotFound)
	}
	data, err = base64.StdEncoding.DecodeString(raw)
	return data, kind, err
}

// RemoveLogo deletes the organisation's logo.
func (s *Store) RemoveLogo(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM settings WHERE key IN ('logo_type', 'logo_data')")
	return err
}

func firstErr(a, b error) error {
	if a != nil {
		return a
	}
	return b
}
