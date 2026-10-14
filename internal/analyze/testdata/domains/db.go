// Package domains holds the hooks and the database. The domains under app/ import it.
package domains

import "database/sql"

var DB *sql.DB
