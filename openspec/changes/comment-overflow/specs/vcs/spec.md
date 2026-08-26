# VCS delta

## ADDED Requirements

### Requirement: Deleting reeve's own comments by marker

The VCS client MUST be able to delete the comments whose body carries a given
marker prefix, so a board that shrinks can remove the parts it no longer needs.

The capability MUST match on both the marker prefix and the comment's author: a
comment is eligible for deletion only when it carries a reeve marker AND was
written by reeve's own authenticated account (its app or bot identity). A comment
written by anyone else MUST NOT be deleted even when its body contains the
marker, which any pull-request participant can copy.
