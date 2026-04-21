package main

# Restrict production resources to US regions. us-east-1, us-east-2,
# us-west-2 are allowed.

allowed_regions := {"us-east-1", "us-east-2", "us-west-2"}

deny[msg] {
  input.env == "prod"
  region := region_from_summary(input.plan_summary)
  not allowed_regions[region]
  msg := sprintf("prod stack %s/%s deploys to disallowed region %s", [input.project, input.stack, region])
}

# Placeholder extractor — in practice this reads the engine's full plan
# artifact instead of the summary string. The reeve-generated plan JSON
# is intentionally a summary, not the raw engine plan.
region_from_summary(summary) = region {
  regex.match(`[a-z]{2}-[a-z]+-[0-9]+`, summary)
  region := regex.find_n(`[a-z]{2}-[a-z]+-[0-9]+`, summary, 1)[0]
}
