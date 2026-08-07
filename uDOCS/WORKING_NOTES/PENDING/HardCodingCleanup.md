I come from a software configuration background where hard coded parameters are frowned upon. So the idea is not to white-label, it is to make sure that nothing that is configurable should require recompiling every time a change is made.


## The governing rule is:

A value that may legitimately change between deployments or during rebranding must not require recompilation.

So this:

allowedCustomSchemes := map[string]struct{}{
	"sagrenti": {},
}


should eventually become configuration-driven, for example:

allowedCustomSchemes := make(map[string]struct{}, len(cfg.OAuth.AllowedCustomSchemes))

for _, scheme := range cfg.OAuth.AllowedCustomSchemes {
	allowedCustomSchemes[strings.ToLower(strings.TrimSpace(scheme))] = struct{}{}
}


With deployment configuration such as:

OAUTH_ALLOWED_CUSTOM_SCHEMES=sagrenti


## This is marked for correction before deployment.