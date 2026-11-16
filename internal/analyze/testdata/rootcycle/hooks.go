package rootcycle

import (
	"context"

	"example.com/rootcycle/app/products"
)

var Names = products.Names

func OnStart(ctx context.Context) error { return nil }
