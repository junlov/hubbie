package profile

import "fmt"

// Guard checks that the cached login matches the profile's expected portal.
// expected and actual are portal ids; client names the fix in the message.
func Guard(client, expected, actual string) error {
	if actual != expected {
		return fmt.Errorf("profile %s expects portal %s but login is for portal %s; repair with hubbie %s auth login --portal %s",
			client, expected, actual, client, expected)
	}
	return nil
}

// ServiceGuard checks the portal a service key belongs to.
func ServiceGuard(client, expected, actual string) error {
	if actual != expected {
		return fmt.Errorf("service key for %s is for portal %s, expected %s; refusing", client, actual, expected)
	}
	return nil
}
