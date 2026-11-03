package vrchat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"unicode"
)

// Ptr supplies an optional request field, including an explicit zero or false.
func Ptr[T any](value T) *T { return &value }

// Call is a named API operation. Construct one using an endpoint function such
// as GetAvatar or SearchWorlds. Client.Call preserves the complete JSON response.
// Use the corresponding client method or Execute for a typed result.
type Call interface {
	build() (string, Request, error)
}

type pathParameter struct{ name, value string }

// Operation binds an endpoint's parameters to its response type. Its fields are
// private so endpoint paths and HTTP methods stay part of the SDK contract.
type Operation[T any] struct {
	method, path       string
	pathParameters     []pathParameter
	parameters         any
	body               any
	header             http.Header
	err                error
	allowEmptyResponse bool
}

func newOperation[T any](method, path string, pathParameters []pathParameter, parameters, body any) Operation[T] {
	if body != nil {
		value := reflect.ValueOf(body)
		if value.Kind() == reflect.Pointer && value.IsNil() {
			body = nil
		}
	}
	return Operation[T]{method: method, path: path, pathParameters: pathParameters, parameters: parameters, body: body}
}

func newEmptyOperation[T any](method, path string, pathParameters []pathParameter, parameters, body any) Operation[T] {
	op := newOperation[T](method, path, pathParameters, parameters, body)
	op.allowEmptyResponse = true
	return op
}

func (op Operation[T]) build() (string, Request, error) {
	if op.err != nil {
		return "", Request{}, op.err
	}
	if op.method == "" || op.path == "" {
		return "", Request{}, errors.New("uninitialized VRChat operation")
	}
	path := op.path
	for _, param := range op.pathParameters {
		if param.value == "" || param.value == "." || param.value == ".." || strings.ContainsFunc(param.value, unicode.IsControl) {
			return "", Request{}, fmt.Errorf("invalid VRChat %s path parameter", param.name)
		}
		path = strings.ReplaceAll(path, "{"+param.name+"}", url.PathEscape(param.value))
	}
	header := op.header.Clone()
	if header == nil {
		header = make(http.Header)
	}
	query, err := encodeParameters(op.parameters, header)
	if err != nil {
		return "", Request{}, err
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	return path, Request{Method: op.method, Body: op.body, Header: header, allowEmptyResponse: op.allowEmptyResponse}, nil
}

func encodeParameters(parameters any, header http.Header) (url.Values, error) {
	query := make(url.Values)
	if parameters == nil {
		return query, nil
	}
	value := reflect.ValueOf(parameters)
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return query, nil
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return nil, errors.New("VRChat parameters must be a struct")
	}
	for i := 0; i < value.NumField(); i++ {
		field, item := value.Type().Field(i), value.Field(i)
		key, isHeader := field.Tag.Get("query"), false
		if key == "" {
			key, isHeader = field.Tag.Get("header"), true
		}
		if key == "" {
			continue
		}
		absent := false
		for item.Kind() == reflect.Pointer {
			if item.IsNil() {
				absent = true
				break
			}
			item = item.Elem()
		}
		if absent || (item.Kind() == reflect.Slice && item.IsNil()) {
			if field.Tag.Get("required") == "true" {
				return nil, fmt.Errorf("VRChat parameter %s is required", key)
			}
			continue
		}
		var values []string
		if item.Kind() == reflect.Slice || item.Kind() == reflect.Array {
			for j := 0; j < item.Len(); j++ {
				values = append(values, fmt.Sprint(item.Index(j).Interface()))
			}
			if isHeader || field.Tag.Get("explode") == "false" {
				values = []string{strings.Join(values, ",")}
			}
		} else {
			values = []string{fmt.Sprint(item.Interface())}
		}
		for _, text := range values {
			if isHeader {
				header.Add(key, text)
			} else {
				query.Add(key, text)
			}
		}
	}
	return query, nil
}

// Call executes a named operation and returns its complete decoded JSON.
// Tokens are returned separately so applications can apply session rotations.
func (c *Client) Call(ctx context.Context, tokens Tokens, operation Call) (any, Tokens, error) {
	if operation == nil || (reflect.ValueOf(operation).Kind() == reflect.Pointer && reflect.ValueOf(operation).IsNil()) {
		return nil, Tokens{}, errors.New("VRChat operation is required")
	}
	path, req, err := operation.build()
	if err != nil {
		return nil, Tokens{}, err
	}
	return c.Do(ctx, path, tokens, req)
}

// Execute returns an operation's typed response. Apply the returned tokens even
// when decoding fails, because VRChat may already have rotated its cookies.
func Execute[T any](ctx context.Context, client *Client, tokens Tokens, operation Operation[T]) (T, Tokens, error) {
	var data T
	path, req, err := operation.build()
	if err != nil {
		return data, Tokens{}, err
	}
	raw, updates, err := client.do(ctx, path, tokens, req)
	if err != nil || raw == nil || (len(raw) == 0 && operation.allowEmptyResponse) {
		return data, updates, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil {
		return data, updates, err
	}
	return data, updates, nil
}
