package notes

import "keepy-go/llm"

// CreateDataSchemaTool creates a tool definition for creating a new data schema.
func CreateDataSchemaTool() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name: "create_data_schema",
		// 强调不要和用户确认schema的结构，自己把握
		Description: "Create a new data schema/book based on user requirements. The schema defines the structure for a type of record (e.g., 'Bill', 'Diary'). Do not ask user for confirmation of the schema structure.",
		Parameters: map[string]interface{}{
			"type": "object",
			"required": []string{
				"name", "schema", "description",
			},
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "The unique type/name of the schema (e.g. 'Bill', 'Diary')",
				},
				"description": map[string]any{
					"type":        "string",
					"description": "Description of what this schema records",
				},
				"schema": map[string]any{
					"type":        "array",
					"description": "List of fields in this schema",
					"items": map[string]any{
						"type":     "object",
						"required": []string{"field", "type", "description"},
						"properties": map[string]any{
							"field": map[string]any{
								"type":        "string",
								"description": "Field name (e.g. 'Amount')",
							},
							"type": map[string]any{
								"type":        "string",
								"description": "Field type (e.g. 'number', 'string', 'date')",
								"enum":        []string{"number", "string", "date"},
							},
							"description": map[string]any{
								"type":        "string",
								"description": "Description of the field",
							},
						},
					},
				},
			},
		},
	}
}

// UpdateDataSchemaTool creates a tool definition for updating an existing data schema.
func UpdateDataSchemaTool() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        "update_data_schema",
		Description: "Update an existing data schema. This can be used to rename the schema, update its description, or modify the fields structure.",
		Parameters: map[string]interface{}{
			"type": "object",
			"required": []string{
				"name", "schema", "description",
			},
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "The unique type/name of the schema to update (e.g. 'Bill', 'Diary')",
				},
				"description": map[string]any{
					"type":        "string",
					"description": "New description of what this schema records",
				},
				"schema": map[string]any{
					"type":        "array",
					"description": "New list of fields in this schema. This will replace the existing fields.",
					"items": map[string]any{
						"type":     "object",
						"required": []string{"field", "type", "description"},
						"properties": map[string]any{
							"field": map[string]any{
								"type":        "string",
								"description": "Field name (e.g. 'Amount')",
							},
							"type": map[string]any{
								"type":        "string",
								"description": "Field type (e.g. 'number', 'string', 'date')",
								"enum":        []string{"number", "string", "date"},
							},
							"description": map[string]any{
								"type":        "string",
								"description": "Description of the field",
							},
						},
					},
				},
			},
		},
	}
}

// DeleteDataSchemaTool creates a tool definition for deleting a data schema.
func DeleteDataSchemaTool() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        "delete_data_schema",
		Description: "Delete an existing data schema.",
		Parameters: map[string]interface{}{
			"type": "object",
			"required": []string{
				"name",
			},
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "The unique type/name of the schema to delete (e.g. 'Bill')",
				},
			},
		},
	}
}

// AddDataRecordTool creates a tool definition for adding a new data record.
func AddDataRecordTool() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        "add_data_record",
		Description: "Add a new record of a specific schema type. The data must strictly follow the schema structure. If the user asks to remind him/her to do something, you should use this tool to add a record with the reminder time.",
		Parameters: map[string]interface{}{
			"type": "object",
			"required": []string{
				"type", "data",
			},
			"properties": map[string]any{
				"type": map[string]any{
					"type":        "string",
					"description": "The schema type of the record (e.g. 'Bill')",
				},
				"data": map[string]any{
					"type":        "string",
					"description": "The JSON string representing the data object. Keys must match the schema fields.",
				},
				"reminder_at": map[string]any{
					"type":        "string",
					"description": "The time to remind the user to do this action if it's a reminder. ISO 8601 format, e.g. '2026-02-07T09:00:00'",
				},
			},
		},
	}
}

// UpdateDataRecordTool creates a tool definition for updating an existing record.
func UpdateDataRecordTool() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        "update_data_record",
		Description: "Update an existing record by ID.",
		Parameters: map[string]interface{}{
			"type": "object",
			"required": []string{
				"id", "data",
			},
			"properties": map[string]any{
				"id": map[string]any{
					"type":        "string",
					"description": "The unique ID of the record to update",
				},
				"data": map[string]any{
					"type":        "string",
					"description": "JSON string of fields to update",
				},
				"reminder_at": map[string]any{
					"type":        "string",
					"description": "The time to remind the user to do this action if it's a reminder. ISO 8601 format, e.g. '2026-02-07T09:00:00'",
				},
			},
		},
	}
}

// DeleteDataRecordTool creates a tool definition for deleting a record.
func DeleteDataRecordTool() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        "delete_data_record",
		Description: "Delete a record by ID.",
		Parameters: map[string]interface{}{
			"type": "object",
			"required": []string{
				"id",
			},
			"properties": map[string]any{
				"id": map[string]any{
					"type":        "string",
					"description": "The unique ID of the record to delete",
				},
			},
		},
	}
}

// URLToMarkdownTool creates a tool definition for converting a URL to markdown.
func URLToMarkdownTool() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        "url_to_markdown",
		Description: "Fetch a URL and convert its web page content to markdown format. Use this when the user provides a URL and wants to read, summarize, or extract information from a web page.",
		Parameters: map[string]interface{}{
			"type": "object",
			"required": []string{
				"url",
			},
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "The URL to fetch and convert to markdown",
				},
			},
		},
	}
}

// WebSearchTool creates a tool definition for searching the web via Google.
func WebSearchTool() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        "web_search",
		Description: "Search the web using Google to find real-time information. Use this when the user asks about current events, facts, or anything that requires up-to-date information from the internet.",
		Parameters: map[string]interface{}{
			"type": "object",
			"required": []string{
				"query",
			},
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The search query string",
				},
			},
		},
	}
}

// GetDataRecordTool creates a tool definition for searching/retrieving records.
func GetDataRecordTool() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        "get_data_record",
		Description: "Search or retrieve records based on query or ID.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Search query string, supports SQL format, and the execution condition is: record_json_string LIKE ?, please think of all possible query conditions, do not need to be exactly the same as the user input",
				},
				"type": map[string]any{
					"type":        "string",
					"description": "Filter by schema type",
				},
				"id": map[string]any{
					"type":        "string",
					"description": "Specific record ID to retrieve",
				},
			},
		},
	}
}
