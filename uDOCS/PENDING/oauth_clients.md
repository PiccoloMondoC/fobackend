func normalizeRedirectURIs(values []string) ([]string, error) {
	if values == nil {
		return []string{}, nil
	}

	allowedCustomSchemes := map[string]struct{}{
		"sagrenti": {},
	}


sagrenti is hard coded above and may need a fix. For example:

        oauth:
        allowed_custom_schemes:
            - sagrenti

        or

        OAUTH_ALLOWED_CUSTOM_SCHEMES=sagrenti


The direction should be:

Brand-agnostic platform design: configurable because the product identity itself is treated as deployment configuration.