-- Random token of the concert's public link /c/<token>; nullable only because ADD COLUMN
-- refuses a non-constant default: every row gets one here or in CreateConcert.
ALTER TABLE concerts ADD COLUMN token TEXT;
UPDATE concerts SET token = lower(hex(randomblob(12)));
CREATE UNIQUE INDEX concerts_token ON concerts (token);
