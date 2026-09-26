/*
Copyright 2024 Nick Merrett.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tools

// Schema helper functions for building JSON Schema objects for tool InputSchema.
// These are convenience wrappers — you can always supply a raw map instead.

// Properties is a map of property name → schema definition.
type Properties map[string]interface{}

// ObjectSchema builds a JSON Schema object with the given properties and required fields.
//
//	tools.ObjectSchema(tools.Properties{
//	    "query":  tools.StringProp("The search query"),
//	    "limit":  tools.IntProp("Max results to return"),
//	}, "query")
func ObjectSchema(props Properties, required ...string) map[string]interface{} {
	schema := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}(props),
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// StringProp returns a JSON Schema string property definition.
func StringProp(description string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "string",
		"description": description,
	}
}

// StringEnumProp returns a JSON Schema string property with an enum constraint.
func StringEnumProp(description string, values ...string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "string",
		"description": description,
		"enum":        values,
	}
}

// IntProp returns a JSON Schema integer property definition.
func IntProp(description string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "integer",
		"description": description,
	}
}

// NumberProp returns a JSON Schema number (float) property definition.
func NumberProp(description string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "number",
		"description": description,
	}
}

// BoolProp returns a JSON Schema boolean property definition.
func BoolProp(description string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "boolean",
		"description": description,
	}
}

// ArrayProp returns a JSON Schema array property definition with the given item type.
func ArrayProp(description, itemType string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "array",
		"description": description,
		"items":       map[string]interface{}{"type": itemType},
	}
}

// ObjectProp returns a JSON Schema free-form object property definition.
func ObjectProp(description string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "object",
		"description": description,
	}
}
