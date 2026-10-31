-- Staged entries: written about branch work, held on the branch until it lands on the base branch.
-- staged_branch is set while the entry is held and cleared at promotion; the rest stay as provenance.
-- staged_landing is the commit that first touched the entry's anchored paths on the branch, pinned once found.
ALTER TABLE entries ADD COLUMN staged_branch TEXT;
ALTER TABLE entries ADD COLUMN staged_base_name TEXT;
ALTER TABLE entries ADD COLUMN staged_base TEXT;
ALTER TABLE entries ADD COLUMN staged_landing TEXT;
ALTER TABLE entries ADD COLUMN stages_over TEXT;
ALTER TABLE entries ADD COLUMN stages_over_version INTEGER;
CREATE INDEX entries_staged ON entries (staged_branch) WHERE staged_branch IS NOT NULL;
