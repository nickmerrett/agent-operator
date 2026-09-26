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

package llm

import (
	"fmt"

	agentv1alpha1 "github.com/nickmerrett/agent-operator/api/v1alpha1"
)

// NewClientFromSpec builds an LLM Client from an LLMProvider spec and its resolved credentials.
// credentials is a map of key→value from the referenced Kubernetes Secret.
func NewClientFromSpec(spec agentv1alpha1.LLMProviderSpec, credentials map[string]string) (Client, error) {
	params := spec.Parameters
	if params == nil {
		params = map[string]string{}
	}

	switch spec.Type {
	case agentv1alpha1.LLMProviderTypeWatsonx:
		apiKey := credentials["api_key"]
		if apiKey == "" {
			apiKey = credentials["apikey"]
		}
		projectID := credentials["project_id"]
		return NewWatsonxClient(spec.Endpoint, spec.Model, apiKey, projectID, params), nil

	case agentv1alpha1.LLMProviderTypeOpenAI:
		apiKey := credentials["api_key"]
		if apiKey == "" {
			apiKey = credentials["apikey"]
		}
		return NewOpenAIClient(spec.Endpoint, spec.Model, apiKey, params), nil

	case agentv1alpha1.LLMProviderTypeOllama:
		// Ollama typically has no API key.
		return NewOllamaClient(spec.Endpoint, spec.Model, params), nil

	case agentv1alpha1.LLMProviderTypeAnthropic:
		// Anthropic uses the same OpenAI-compatible interface via the messages API.
		// The endpoint should be set to https://api.anthropic.com
		apiKey := credentials["api_key"]
		if apiKey == "" {
			apiKey = credentials["apikey"]
		}
		return NewOpenAIClient(spec.Endpoint, spec.Model, apiKey, params), nil

	default:
		return nil, fmt.Errorf("unsupported LLM provider type: %q", spec.Type)
	}
}
