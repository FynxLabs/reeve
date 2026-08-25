# VCS delta

## ADDED Requirements

### Requirement: Deleting reeve's own comments by marker

The VCS client MUST be able to delete the comments whose body carries a given
marker prefix, so a board that shrinks can remove the parts it no longer needs.

The capability MUST be scoped to markers, which only reeve's own comments carry.
It MUST NOT be able to delete a comment written by anyone else.
