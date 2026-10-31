ALTER TABLE gaps ADD COLUMN resolved_by TEXT;
UPDATE gaps SET resolved_by = session_id WHERE state = 'resolved';
INSERT INTO settings (key, value) VALUES ('gap_close_cap', '10');
