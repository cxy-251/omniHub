package games

import (
	"context"
	"errors"
	"fmt"
)

// ListCatalogPage reads at most 100 display records after an ID. Launch-only
// argv/environment/executable fields are omitted. Oversized stored text becomes
// NULL before scan rather than allocating unbounded values from a foreign DB.
func (d *DB) ListCatalogPage(ctx context.Context, target, category, after string, limit int) (result []*Game, resultErr error) {
	if (target != "gui_only" && target != "web_only") || limit < 1 || limit > 100 || len(after) > 512 || (category != "" && !ValidCategory(category)) {
		return nil, errors.New("invalid game catalog bounds")
	}
	relDir := "''"
	if d.hasRelDir {
		relDir = boundedCatalogColumn("rel_dir", 4096)
	}
	query := "SELECT " + boundedCatalogColumn("id", 512) + "," + boundedCatalogColumn("name", 4096) + "," + boundedCatalogColumn("category", 32) + ",target," + boundedCatalogColumn("icon", 4096) + ",peak_rss_mb,peak_io_mbps,play_time_sec," + relDir + " FROM games WHERE target=? AND id>?"
	arguments := []any{target, after}
	if category != "" {
		query += " AND category=?"
		arguments = append(arguments, category)
	}
	query += " ORDER BY id LIMIT ?"
	arguments = append(arguments, limit)
	rows, err := d.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, rows.Close()) }()
	result = make([]*Game, 0, limit)
	for rows.Next() {
		var game Game
		if err := rows.Scan(&game.ID, &game.Name, &game.Category, &game.Target, &game.Icon, &game.PeakRSSMB, &game.PeakIOMBps, &game.PlayTimeSec, &game.RelDir); err != nil {
			return nil, err
		}
		if err := ValidateIndexGame(&game); err != nil {
			return nil, err
		}
		result = append(result, &game)
	}
	return result, rows.Err()
}

func boundedCatalogColumn(name string, limit int) string {
	return fmt.Sprintf("CASE WHEN length(CAST(%s AS BLOB))<=%d THEN %s ELSE NULL END", name, limit, name)
}
