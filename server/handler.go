package server

import (
	"errors"
	"net/http"

	"github.com/liustack/tifa"
)

// The generated routes.go registers a method handler that takes an input or returns a result
// through these adapters. bind is (*tifa.Ctx).Bind for a request body and (*tifa.Ctx).BindQuery
// for the query string of GET and DELETE.

// Input decodes the input with bind, validates it, calls h and answers 204 when h succeeds.
func Input[In any](h func(*tifa.Ctx, In) error, bind func(*tifa.Ctx, any) error) func(*tifa.Ctx) error {
	return func(c *tifa.Ctx) error {
		var in In
		if err := decode(c, &in, bind); err != nil {
			return err
		}
		if err := h(c, in); err != nil {
			return err
		}
		return c.NoContent(http.StatusNoContent)
	}
}

// Output calls h and answers 200 with its result as JSON.
func Output[Out any](h func(*tifa.Ctx) (Out, error)) func(*tifa.Ctx) error {
	return func(c *tifa.Ctx) error {
		out, err := h(c)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, out)
	}
}

// InputOutput decodes the input with bind, validates it, calls h and answers 200 with its result
// as JSON.
func InputOutput[In, Out any](h func(*tifa.Ctx, In) (Out, error), bind func(*tifa.Ctx, any) error) func(*tifa.Ctx) error {
	return func(c *tifa.Ctx) error {
		var in In
		if err := decode(c, &in, bind); err != nil {
			return err
		}
		out, err := h(c, in)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, out)
	}
}

// decode fills in and runs its Validate method, which tifa build has checked to be
// Validate() error when the input type has one. A tifa.Error or tifa.Fields from Validate is
// answered as is, any other error with 400 and its message.
func decode(c *tifa.Ctx, in any, bind func(*tifa.Ctx, any) error) error {
	if err := bind(c, in); err != nil {
		return err
	}
	v, ok := in.(interface{ Validate() error })
	if !ok {
		return nil
	}
	err := v.Validate()
	if err == nil || errors.As(err, new(tifa.Error)) || errors.As(err, new(*tifa.Error)) {
		return err
	}
	if _, ok := fieldsOf(err); ok {
		return err
	}
	return tifa.Error{Status: http.StatusBadRequest, Message: err.Error()}
}

// fieldsOf finds the tifa.Fields in err, held as a value or a pointer, wrapped or not.
func fieldsOf(err error) (tifa.Fields, bool) {
	var fields tifa.Fields
	if errors.As(err, &fields) {
		return fields, true
	}
	var ptr *tifa.Fields
	if errors.As(err, &ptr) && ptr != nil {
		return *ptr, true
	}
	return nil, false
}

func isFields(err error) bool {
	_, ok := fieldsOf(err)
	return ok
}
