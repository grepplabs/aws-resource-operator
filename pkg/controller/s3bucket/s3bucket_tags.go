package s3bucket

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"regexp"
)

func tagsToMapS3(ts []*s3.Tag) map[string]string {
	result := make(map[string]string)
	for _, t := range ts {
		if !tagIgnoredS3(t) {
			result[*t.Key] = *t.Value
		}
	}

	return result
}

// compare a tag against a list of strings and checks if it should
// be ignored or not
func tagIgnoredS3(t *s3.Tag) bool {
	filter := []string{"^aws:"}
	for _, v := range filter {
		r, _ := regexp.MatchString(v, *t.Key)
		if r {
			return true
		}
	}
	return false
}

func diffTagsS3(oldTags, newTags map[string]string) (map[string]string, map[string]string) {
	// Build the list of what to remove
	remove := make(map[string]string)
	for k, v := range oldTags {
		oldV, ok := newTags[k]
		if !ok || oldV != v {
			// Delete it!
			remove[k] = v
		}
	}
	return newTags, remove
}

// tagsFromMap returns the tags for the given map of data.
func tagsFromMapS3(m map[string]string) []*s3.Tag {
	result := make([]*s3.Tag, 0, len(m))
	for k, v := range m {
		t := &s3.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		}
		if !tagIgnoredS3(t) {
			result = append(result, t)
		}
	}

	return result
}
