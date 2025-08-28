example1 and example2 eacho use a local actions that have the same
path (actions/docker-local) but do not behave the same. This verifies
that the locally built images have different names and do not collide
despite both being called with `uses: ./actions/docker-local.
