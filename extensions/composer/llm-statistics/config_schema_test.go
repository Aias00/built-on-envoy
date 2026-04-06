// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package llmstatistics

import (
	"testing"

	internaltesting "github.com/tetratelabs/built-on-envoy/extensions/composer/internal/testing"
)

func TestConfigSchema(t *testing.T) {
	t.Run("valid full config", func(t *testing.T) {
		internaltesting.AssertSchemaValid(t, "config.schema.json", `{
			"metadata_namespace": "custom.namespace",
			"use_default_attributes": true,
			"use_default_response_attributes": false,
			"session_id_header": "x-session-id"
		}`)
	})

	t.Run("empty config", func(t *testing.T) {
		internaltesting.AssertSchemaValid(t, "config.schema.json", `{}`)
	})

	t.Run("invalid wrong type", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{
			"use_default_attributes": "yes"
		}`)
	})

	t.Run("invalid unknown field", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{
			"unknown_field": "value"
		}`)
	})
}
