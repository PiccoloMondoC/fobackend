### The code below hard codes these values. GPT promises to fix this before deployment.
				'http://localhost:4200/auth/callback',
				'https://sagrenti.com/auth/callback'

	// ---------------------------------------------------------------
	// oauth_clients
	//
	// OAuth client seed secrets are supplied by bootstrap after startup
	// configuration validation. Plaintext client secrets must not be embedded
	// in source, SQL literals, logs, traces, metrics, audit payloads, or public
	// JSON. This query accepts secrets only as bind parameters and stores only
	// pgcrypto-derived password hashes.
	// ---------------------------------------------------------------
	insertOAuthClientQuery = `
	INSERT INTO oauth_clients (
		client_id,
		client_secret_hash,
		is_active,
		allowed_redirect_uris
	) VALUES
		(
			'sd-web-client',
			crypt($1, gen_salt('bf', 10)),
			TRUE,
			ARRAY[
				'http://localhost:4200/auth/callback',
				'https://sagrenti.com/auth/callback'
			]
		),
		(
			'sd-mobile-client',
			crypt($2, gen_salt('bf', 10)),
			TRUE,
			ARRAY[
				'sagrenti://auth/callback'
			]
		)
	ON CONFLICT (client_id) DO NOTHING;
	`
