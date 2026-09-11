# Scenario annotations

`# expect` names an observed contract behavior and ends with its rule tags.
`# setup` describes a driver fixture acknowledgement (writing a fixture file or
setting an override), which is not itself a configuration contract claim.

Both kinds remain in the complete `.expected` transcript. The Go and Python
corpus tests compare every output byte against that file, including setup
acknowledgements. Changing an annotation does not remove a transcript check.
