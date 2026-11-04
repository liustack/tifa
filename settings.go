package tifa

import (
	"time"

	"github.com/liustack/tifa/internal/config"
)

// String defines a setting of the app and returns its value. Write it in a package-level var
// of config.go in the root of the app, where tifa build looks for it:
//
//	var DBURL = tifa.String("db.url", "app.db", "Database file")
//
// The key is lower case letters, digits and underscores, in at least two parts joined with
// dots. Its flag is --db-url and its environment variable DB_URL. The value comes from the
// first of these that sets it: the flag, the environment variable, the .env file of the working
// directory, the file of -c, and then def. An invalid value gives def, and the program lists
// it with the other invalid settings and stops before OnStart. In a test binary the value is
// def, and a test sets the variable itself.
func String(key, def, help string) string { return config.DefineString(key, def, help) }

// Strings defines a setting of the app whose value is a list, written as comma-separated text
// such as a.example,b.example, see String. Spaces around each item are dropped, empty text is
// no items, and an empty item between commas is invalid.
func Strings(key string, def []string, help string) []string {
	return config.DefineStrings(key, def, help)
}

// Int defines a setting of the app that is a whole number, see String.
func Int(key string, def int, help string) int { return config.DefineInt(key, def, help) }

// Bool defines a setting of the app that is true or false, see String. Its flag alone, such as
// --shop-open, is true, and takes a value only after =: --shop-open=false.
func Bool(key string, def bool, help string) bool { return config.DefineBool(key, def, help) }

// Duration defines a setting of the app that is a duration in Go's format, such as 3s or 1m30s,
// see String.
func Duration(key string, def time.Duration, help string) time.Duration {
	return config.DefineDuration(key, def, help)
}

// Size defines a setting of the app that is a number of bytes, see String. It is written as a
// whole number with an optional unit B, KiB, MiB or GiB, such as 512KiB.
func Size(key string, def int64, help string) int64 { return config.DefineSize(key, def, help) }

// Secret defines a setting of the app that is a secret, such as a password or an API token, and
// returns its value, empty when no source sets it. It comes from the environment variable, the
// .env file of the working directory or the file of -c, see String. A flag would show it in the
// process list to every user of the machine, so the program refuses one and says where to put
// it. config prints where a secret came from and never its value.
//
//	var SMTPPassword = tifa.Secret("smtp.password", "SMTP password")
func Secret(key, help string) string { return config.DefineSecret(key, help) }
