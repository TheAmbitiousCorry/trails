-- The default cap moves from seven to eight; a project still on the old default follows, a project that chose its own value keeps it.
UPDATE settings SET value = '8' WHERE key = 'fan_out_cap' AND value = '7';
