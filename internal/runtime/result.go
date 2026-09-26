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

package runtime

// Result holds the outcome of a completed agentic loop run.
type Result struct {
	// Answer is the agent's final text response (non-empty on success).
	Answer string `json:"result"`
	// TokensUsed is the total number of tokens consumed during this run.
	TokensUsed int `json:"tokensUsed"`
	// Iterations is the number of reasoning loop iterations completed.
	Iterations int `json:"iterations"`
	// Error is non-empty when the run failed or timed out.
	Error string `json:"error,omitempty"`
}
