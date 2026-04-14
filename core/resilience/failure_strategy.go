package resilience

type DefaultFailureStrategy struct{}

func (s *DefaultFailureStrategy) IsRetryableError(err error) bool {
	return IsRetryable(ClassifyError(err))
}
