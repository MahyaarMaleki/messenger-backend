package util

// StringOrEmpty safely dereferences a string pointer
func StringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}
