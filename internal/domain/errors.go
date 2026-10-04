package domain

import "errors"

var (
	ErrInsufficientResources = errors.New("node capacity full")
	ErrContainerNotFound     = errors.New("container not found")
	ErrInvalidInput          = errors.New("invalid input")
	ErrConflict              = errors.New("conflict")
)
