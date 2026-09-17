package provider

// Callouts shared between resource descriptions. They are constants because
// CI fails on any docs diff, so prose that appears on a dozen generated pages
// needs one place to edit.
const (
	// adoptNote belongs on every resource whose object the API can neither
	// create nor delete. There are several, and users reasonably expect a
	// Terraform resource to own the thing it names.
	adoptNote = "~> **This resource does not create or destroy anything.** The zone.eu API has no " +
		"endpoint for either: the object exists because a domain or service was bought, and it goes " +
		"away with it. Applying this resource adopts what already exists and manages its settings; " +
		"destroying it stops managing those settings and leaves everything in place."

	// unverifiedWriteNote marks a write path that has never run against the
	// live API. Saying so is better than letting a user discover it.
	unverifiedWriteNote = "~> **The update path for this resource is unverified.** zone.eu's published API " +
		"description declares no request body for it, so the payload is inferred from the shape of a " +
		"read, and exercising it would have changed a production service. It may need correcting; " +
		"please report what happens."
)
