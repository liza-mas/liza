# Cache entries need a size limit

**Trigger:** Adding a value to a cache or store.

A cache that accepts any value grows until it is the largest consumer of memory. Give every
cache a size bound when it is created, not when the first incident shows the need.
