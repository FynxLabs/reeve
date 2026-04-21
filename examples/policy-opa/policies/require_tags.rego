package main

# Every production stack must declare at least one resource tagged with
# cost-center and owner. The reeve-generated plan JSON carries `env` and
# `counts`; richer policies need to read the engine's own plan artifact.

deny[msg] {
  input.env == "prod"
  input.counts.add + input.counts.change > 0
  not plan_has_cost_center
  msg := sprintf("prod stack %s/%s: new/changed resources require cost-center tag", [input.project, input.stack])
}

deny[msg] {
  input.env == "prod"
  input.counts.add + input.counts.change > 0
  not plan_has_owner
  msg := sprintf("prod stack %s/%s: new/changed resources require owner tag", [input.project, input.stack])
}

plan_has_cost_center {
  contains(input.plan_summary, "cost-center")
}

plan_has_owner {
  contains(input.plan_summary, "owner")
}
