package workspace

import "context"

// Provider is the Go-owned boundary for page and layout data requirements.
// Output describes the serializable DTO. Load is called before entering QuickJS.
type Provider struct {
	Params []string
	Output any
	Load   func(context.Context, map[string]string) (any, error)
}

func Providers(store *Store) map[string]Provider {
	return map[string]Provider{
		"shell": {Params: []string{}, Output: ShellData{}, Load: func(ctx context.Context, _ map[string]string) (any, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return store.Shell(), nil
		}},
		"projects.detail": {Params: []string{"projectId"}, Output: Project{}, Load: func(ctx context.Context, p map[string]string) (any, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return store.Project(p["projectId"])
		}},
		"orders.list": {Params: []string{"projectId"}, Output: OrderList{}, Load: func(ctx context.Context, p map[string]string) (any, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return store.Orders(p["projectId"])
		}},
		"orders.detail": {Params: []string{"projectId", "orderId"}, Output: Order{}, Load: func(ctx context.Context, p map[string]string) (any, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return store.Order(p["projectId"], p["orderId"])
		}},
	}
}
