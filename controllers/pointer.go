package controllers

// BoolPtr returns a pointer to the specified boolean value.
func BoolPtr(value bool) *bool {
	return &value
}

// Int32Ptr returns a pointer to the specified integer value.
func Int32Ptr(value int32) *int32 {
	return &value
}
