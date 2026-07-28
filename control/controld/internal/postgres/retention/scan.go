package pgretention

import "fmt"

type stringRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanStrings(rows stringRows, label string) ([]string, error) {
	out := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan %s: %w", label, err)
		}
		out = append(out, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", label, err)
	}
	return out, nil
}
