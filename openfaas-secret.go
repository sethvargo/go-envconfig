package envconfig

import "io/ioutil"

func getAPISecretBytes(secretName string) (secretBytes []byte, err error) {
	// read from the openfaas secrets folder
	secretBytes, err = ioutil.ReadFile("/var/openfaas/secrets/" + secretName)
	if err != nil {
		// read from the original location for backwards compatibility with openfaas <= 0.8.2
		secretBytes, err = ioutil.ReadFile("/run/secrets/" + secretName)
	}
	return secretBytes, err
}

// getAPISecret get secret as a string or empty string if not exists
func getAPISecret(secretName string) string {
	secret, err := getAPISecretBytes(secretName)
	if err != nil {
		return ""
	}
	return string(secret)
}
