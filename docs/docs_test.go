package docs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSwaggerInfo(t *testing.T) {
	assert.NotNil(t, SwaggerInfo)
	assert.Equal(t, "Litepod API", SwaggerInfo.Title)
	assert.Equal(t, "/", SwaggerInfo.BasePath)
}
