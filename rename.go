package jsontransform

import "github.com/iancoleman/strcase"

// SnakeCaseRename returns a RenameFunc that converts all field names to snake_case.
// Example: "UserName" → "user_name"
func SnakeCaseRename() RenameFunc {
	return func(name string) *string {
		s := strcase.ToSnake(name)
		return &s
	}
}

// CamelCaseRename returns a RenameFunc that converts all field names to lowerCamelCase.
// Example: "user_name" → "userName"
func CamelCaseRename() RenameFunc {
	return func(name string) *string {
		s := strcase.ToLowerCamel(name)
		return &s
	}
}

// PascalCaseRename returns a RenameFunc that converts all field names to PascalCase.
// Example: "user_name" → "UserName"
func PascalCaseRename() RenameFunc {
	return func(name string) *string {
		s := strcase.ToCamel(name)
		return &s
	}
}

// KebabCaseRename returns a RenameFunc that converts all field names to kebab-case.
// Example: "UserName" → "user-name"
func KebabCaseRename() RenameFunc {
	return func(name string) *string {
		s := strcase.ToKebab(name)
		return &s
	}
}

// MapRename returns a RenameFunc that renames fields according to the given map.
// Fields not found in the map are not renamed (returns nil).
// Can be combined with other RenameFuncs for fallback behavior.
func MapRename(m map[string]string) RenameFunc {
	return func(name string) *string {
		if newName, ok := m[name]; ok {
			return &newName
		}
		return nil
	}
}
