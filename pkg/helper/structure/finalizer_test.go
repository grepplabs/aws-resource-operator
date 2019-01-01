package structure

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestContainsString(t *testing.T) {
	a := assert.New(t)
	a.False(ContainsString([]string{}, ""))
	a.False(ContainsString([]string{"delete-bucket.finalizers.aws.grepplabs.com"}, ""))
	a.True(ContainsString([]string{"delete-bucket.finalizers.aws.grepplabs.com"}, "delete-bucket.finalizers.aws.grepplabs.com"))
}

func TestRemoveString(t *testing.T) {
	a := assert.New(t)
	a.Equal([]string{"delete-bucket.finalizers.aws.grepplabs.com"}, RemoveString([]string{"delete-bucket.finalizers.aws.grepplabs.com"}, ""))
	a.Equal([]string(nil), RemoveString([]string(nil), "delete-bucket.finalizers.aws.grepplabs.com"))
	a.Equal([]string(nil), RemoveString([]string{"delete-bucket.finalizers.aws.grepplabs.com"}, "delete-bucket.finalizers.aws.grepplabs.com"))
	a.Equal([]string{"delete-bucket.finalizers.aws.grepplabs.com"}, RemoveString([]string{"delete-bucket.finalizers.aws.grepplabs.com"}, "other-string"))
}
