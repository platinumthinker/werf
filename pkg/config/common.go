package config

import (
	"fmt"
	"strings"

	"github.com/flant/dapp/pkg/util"
)

type RawOrigin interface {
	ConfigSection() interface{}
	Doc() *Doc
}

type Doc struct {
	Content        []byte
	Line           int
	RenderFilePath string
}

func CheckOverflow(m map[string]interface{}, config interface{}) error {
	if len(m) > 0 {
		var keys []string
		for k := range m {
			keys = append(keys, k)
		}

		// val := reflect.Indirect(reflect.ValueOf(config))                   // FIXME: access to raw object needed
		return fmt.Errorf("Unknown fields: `%s`", strings.Join(keys, "`, `")) // FIXME: access to raw object needed
	}
	return nil
}

func AllRelativePaths(paths []string) bool {
	if len(paths) == 0 {
		return true
	} else {
		return !AllAbsolutePaths(paths)
	}
}

func AllAbsolutePaths(paths []string) bool {
	for _, path := range paths {
		if isRelativePath(path) {
			return false
		}
	}
	return true
}

func isRelativePath(path string) bool {
	return !IsAbsolutePath(path)
}

func IsAbsolutePath(path string) bool {
	return strings.HasPrefix(path, "/")
}

func InterfaceToStringArray(stringOrStringArray interface{}) ([]string, error) {
	if stringOrStringArray == nil {
		return []string{}, nil
	} else if val, ok := stringOrStringArray.(string); ok {
		return []string{val}, nil
	} else if interfaceArray, ok := stringOrStringArray.([]interface{}); ok {
		stringArray := []string{}
		for _, interf := range interfaceArray {
			if val, ok := interf.(string); ok {
				stringArray = append(stringArray, val)
			} else {
				return nil, fmt.Errorf("ожидается строка или массив строк: %v", stringOrStringArray) // FIXME
			}
		}
		return stringArray, nil
	} else {
		return nil, fmt.Errorf("ожидается строка или массив строк: %v", stringOrStringArray) // FIXME
	}
}

// Stack for setting parents in UnmarshalYAML calls
// Set this to util.NewStack before yaml.Unmarshal
var ParentStack *util.Stack
