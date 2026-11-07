package exitcode

const WarningStatus = -1

type CodeInfo struct {
	Code    string
	Status  int
	Meaning string
}

type StatusInfo struct {
	Status  int
	Meaning string
}

var Statuses = []StatusInfo{
	{OK, "success"},
	{Usage, "usage error: a wrong flag, command or argument"},
	{Database, "database error: the table, the column or the key does not fit, or the contract diverges"},
	{Data, "data error: a value, a sample, an invariant or a record is refused"},
	{File, "file error: a batch, rules or input file is missing or has an invalid format"},
	{System, "system error: disk, permission, lock, I/O, a signal; an error Scribo did not classify"},
}

var Codes = []CodeInfo{
	{CodeUsage, Usage, "a flag, command or argument is wrong"},
	{CodeTableNotFound, Database, "the table does not exist"},
	{CodeColumnNotFound, Database, "a column does not exist in the table"},
	{CodeReservedTable, Database, "the table belongs to Scribo or Colligo"},
	{CodeKeyMismatch, Database, "the key is not exactly one unique key of the table"},
	{CodeNoUniqueKey, Database, "the table has no unique key to name its records"},
	{CodeUntypedComparison, Database, "a numeric comparison against a column without a type"},
	{CodeColligoRequired, Database, "the command needs Colligo's tables in the database"},
	{CodeContractMismatch, Database, "a contract table differs from CONTRACT.md"},
	{CodeKeyNotFound, Data, "no record has this key"},
	{CodeKeyAlreadyExists, Data, "the record already exists and --insert-only was given"},
	{CodeInvalidValue, Data, "a value does not fit its column"},
	{CodeUnknownSample, Data, "the sample is not in Colligo's samples"},
	{CodeInvariantViolated, Data, "a rule of scribo.rules.toml is violated"},
	{CodeStaleResults, Data, "stale results were found"},
	{CodeNondeterministic, Data, "a result changed with identical inputs and tool"},
	{CodeConstraintFailed, Data, "the database refused a value: NOT NULL, UNIQUE, CHECK or a trigger"},
	{CodeAmbiguousKey, Data, "the key matches more than one record"},
	{CodeRunNotFound, Data, "the run wrote nothing in this table"},
	{CodeInputNotFound, File, "an --input file does not exist"},
	{CodeInvalidRules, File, "scribo.rules.toml cannot be read or parsed"},
	{CodeFileNotFound, File, "the database or another file does not exist"},
	{CodeInvalidFormat, File, "the file is not in the expected format"},
	{CodeFileExists, File, "the output file exists and --overwrite was not given"},
	{CodeBatchFailed, File, "a line of the batch failed; nothing was written"},
	{CodeDBBusy, System, "another process holds the write lock"},
	{CodePermissionDenied, System, "the file cannot be read or written"},
	{CodeSyncedPath, System, "the database is in a synced folder"},
	{CodePragmaIneffective, System, "a PRAGMA did not take effect"},
	{CodeInternal, System, "an error Scribo did not classify: a bug to report"},
	{CodeIOError, System, "a database error with no more specific code"},
	{CodeInterrupted, System, "a signal ended the command; nothing was written"},
	{CodeRuleUnavailable, WarningStatus, "a rule needs an input outside Colligo's custody; not evaluated"},
	{CodeNetworkFilesystem, WarningStatus, "the database is on a network filesystem; journal mode DELETE"},
	{CodeHistoryReconciled, WarningStatus, "a change made outside Scribo was found and recorded"},
	{CodeHistoryApproximate, WarningStatus, "the state at that moment is approximate"},
	{CodeToolVersionUnknown, WarningStatus, "the file does not record the tool version"},
}
