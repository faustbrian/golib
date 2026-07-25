package labelmodel

type UserID string
type RequestPath string
type LowCardinality string
type LabelName string

func Bucket(UserID) string { return "bucket" }
