package s3bucket

import (
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
)

func TestDiffTagsS3(t *testing.T) {
	cases := []struct {
		Old, New       map[string]string
		Create, Remove map[string]string
	}{
		// Basic add/remove
		{
			Old: map[string]string{
				"foo": "bar",
			},
			New: map[string]string{
				"bar": "baz",
			},
			Create: map[string]string{
				"bar": "baz",
			},
			Remove: map[string]string{
				"foo": "bar",
			},
		},

		// Modify
		{
			Old: map[string]string{
				"foo": "bar",
			},
			New: map[string]string{
				"foo": "baz",
			},
			Create: map[string]string{
				"foo": "baz",
			},
			Remove: map[string]string{
				"foo": "bar",
			},
		},
	}

	for i, tc := range cases {
		c, r := diffTagsS3(tc.Old, tc.New)
		cm := tagsToMapS3(tagsFromMapS3(c))
		rm := tagsToMapS3(tagsFromMapS3(r))
		if !reflect.DeepEqual(cm, tc.Create) {
			t.Fatalf("%d: bad create: %#v", i, cm)
		}
		if !reflect.DeepEqual(rm, tc.Remove) {
			t.Fatalf("%d: bad remove: %#v", i, rm)
		}
	}
}

func TestIgnoringTagsS3(t *testing.T) {
	var ignoredTags []*s3.Tag
	ignoredTags = append(ignoredTags, &s3.Tag{
		Key:   aws.String("aws:cloudformation:logical-id"),
		Value: aws.String("foo"),
	})
	ignoredTags = append(ignoredTags, &s3.Tag{
		Key:   aws.String("aws:foo:bar"),
		Value: aws.String("baz"),
	})
	for _, tag := range ignoredTags {
		if !tagIgnoredS3(tag) {
			t.Fatalf("Tag %v with value %v not ignored, but should be!", *tag.Key, *tag.Value)
		}
	}
}
