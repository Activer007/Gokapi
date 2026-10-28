# SEC-032 managed upload validation

Production baseline: 3d93cdb. Final DKXgX6: environment, generation, scoped contracts, awsmock, noaws, integration and related race all exit 0. The test archive was a private assessment descendant of this baseline; test-archive-head.txt records its actual HEAD, with exact public Go overlays in source.sha256. Private request tests and their raw output are excluded from this release.

Managed request contract: 56 passing leaf cases; File Request contract: 34 passing leaf cases. Original defensive RED and a six-failure cleanup mutation are retained privately. Public runner reproduces public regression tests and standard suites. This evidence does not claim real-browser/real-backend or all asynchronous-failure coverage.
