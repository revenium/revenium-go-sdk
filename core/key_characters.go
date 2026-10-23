package core

// ValidateAPIKeyCharactersFor rejects a key that cannot travel in an HTTP
// header, naming setting as the place the key came from. The key itself is
// never echoed, because it is a secret.
func ValidateAPIKeyCharactersFor(setting, apiKey string) error {
	return validateKeyCharacters(setting, apiKey)
}
