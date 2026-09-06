package xlwms

import (
	"context"
	"errors"
	"fmt"
)

// DiscoverInventory queries the complete credential scope, without a warehouse filter.
func (c *Client) DiscoverInventory(ctx context.Context) ([]map[string]any, error) {
	records := make([]map[string]any, 0)
	pages := 1
	for page := 1; page <= pages; page++ {
		if page > 1000 {
			return nil, errors.New("API inventory discovery exceeded 1000 pages")
		}
		result, err := c.PageInventory(ctx, "integrated", map[string]any{}, page, 100)
		if err != nil {
			return nil, err
		}
		data, ok := result["data"].(map[string]any)
		if !ok {
			return nil, errors.New("API inventory response is missing data")
		}
		batch, ok := data["records"].([]any)
		if !ok {
			return nil, errors.New("API inventory response has invalid records")
		}
		for _, raw := range batch {
			record, ok := raw.(map[string]any)
			if !ok {
				return nil, errors.New("API inventory response contains an invalid record")
			}
			records = append(records, record)
		}
		if page == 1 {
			pages = inventoryPageInteger(data["pages"])
			if pages < 1 {
				pages = (inventoryPageInteger(data["total"]) + 99) / 100
				if pages < 1 {
					pages = 1
				}
			}
		}
	}
	return records, nil
}

func inventoryPageInteger(value any) int {
	var result int
	_, _ = fmt.Sscan(fmt.Sprint(value), &result)
	return result
}
