package ssr

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"math"

	"github.com/liustack/slowjs"
)

// Read on each call, including calls made while loading modules. There is no pool
// of random bytes to retain per VM or accidentally reuse between renders.
func registerRandom(vm *slowjs.VM) error {
	return vm.RegisterHostFunc("__tifa_random_bytes__", func(args []any) (any, error) {
		if len(args) != 1 {
			return nil, errors.New("random bytes requires a length")
		}
		n, ok := args[0].(float64)
		if i, integral := args[0].(int); integral {
			n, ok = float64(i), true
		}
		if !ok || n < 0 || n > 65536 || n != math.Trunc(n) {
			return nil, errors.New("random byte length must be an integer from 0 to 65536")
		}
		buf := make([]byte, int(n))
		if _, err := rand.Read(buf); err != nil {
			return nil, err
		}
		return base64.StdEncoding.EncodeToString(buf), nil
	})
}
