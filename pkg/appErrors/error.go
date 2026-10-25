package appErrors

import "fmt"

// AppError is the error shown in the UI: a short title, an actionable
// description and the original wrapped error.
type AppError struct {
	Title       string
	Description string
	Err         error
}

func (ae *AppError) Error() string {
	if ae.Err != nil {
		return fmt.Sprintf("%s: %s (%v)", ae.Title, ae.Description, ae.Err)
	}
	return fmt.Sprintf("%s: %s", ae.Title, ae.Description)
}

func (ae *AppError) Unwrap() error {
	return ae.Err
}

func Error(title, description string, err error) error {
	return &AppError{
		Title:       title,
		Description: description,
		Err:         err,
	}
}
