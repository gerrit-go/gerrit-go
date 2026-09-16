export default {
  submitBehaviour: "Submit behaviour",
  submitType: "Submit type",
  submitTypeHelp: {
    FAST_FORWARD_ONLY: "Only allow submits that fast-forward the target branch.",
    REBASE_IF_NECESSARY:
      "Rebase the change onto the target branch when a fast-forward is not possible.",
    REBASE_ALWAYS: "Always rebase the change onto the target branch before submitting.",
    MERGE_IF_NECESSARY: "Create a merge commit when a fast-forward is not possible.",
    MERGE_ALWAYS: "Always create a merge commit, even when a fast-forward is possible.",
    CHERRY_PICK: "Cherry-pick the change onto the target branch, creating a new commit.",
  },
  submitWholeTopic: "Submit whole topic",
  wholeTopicHint: "(submitting one change also submits all open changes sharing its topic)",
  submitRequirements: "Submit requirements",
  requirementsHint:
    "A change is submittable when every requirement's label reaches its minimum value and no vote is at or below its block value.",
  noRequirements: "No submit requirements configured.",
  addRequirement: "Add requirement",
  removeRequirement: "Remove requirement",
  label: "Label",
  minValue: "Min value",
  blockValue: "Block value",
  saved: "Saved.",
};
