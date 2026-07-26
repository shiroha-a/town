package content

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrImageNotFound is returned when an uploaded image name does not exist.
var ErrImageNotFound = errors.New("image not found")

// SaveImage upserts an uploaded image (admin). name is a URL slug.
func (s *Service) SaveImage(ctx context.Context, name, mime string, data []byte) error {
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO uploaded_images (name, mime, data) VALUES ($1, $2, $3)
		 ON CONFLICT (name) DO UPDATE SET mime = $2, data = $3, created_at = now()`,
		name, mime, data); err != nil {
		return fmt.Errorf("save image: %w", err)
	}
	return nil
}

// GetImage returns the mime type and bytes of an uploaded image.
func (s *Service) GetImage(ctx context.Context, name string) (string, []byte, error) {
	var mime string
	var data []byte
	err := s.pool.QueryRow(ctx,
		`SELECT mime, data FROM uploaded_images WHERE name = $1`, name).Scan(&mime, &data)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, ErrImageNotFound
	}
	if err != nil {
		return "", nil, fmt.Errorf("get image: %w", err)
	}
	return mime, data, nil
}

// ImageInUse reports whether an uploaded image is placed on the background layer
// (town_map.assets の img が 'u:'+name のマスがあるか)。
func (s *Service) ImageInUse(ctx context.Context, name string) (bool, error) {
	var used bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(
		   SELECT 1 FROM town_map, jsonb_array_elements(assets) a
		   WHERE id = 1 AND a->>'img' = $1)`, "u:"+name).Scan(&used)
	if err != nil {
		return false, fmt.Errorf("check image use: %w", err)
	}
	return used, nil
}

// DeleteImage removes an uploaded image by name.
func (s *Service) DeleteImage(ctx context.Context, name string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM uploaded_images WHERE name = $1`, name); err != nil {
		return fmt.Errorf("delete image: %w", err)
	}
	return nil
}

// ListImageNames returns every uploaded image name (for the asset palette).
func (s *Service) ListImageNames(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT name FROM uploaded_images ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list images: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("scan image name: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
