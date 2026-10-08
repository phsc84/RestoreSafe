// Package e2e drives the interactive backup, verify, and restore workflows end
// to end with scripted answers.
//
// # Fault-injection matrix
//
// Each fault, and the test that covers it per operation. Tests without a
// package name are in this package. "–" means the fault cannot reach the
// operation.
//
//	Fault                               Backup                                         Restore                                       Verify
//	----------------------------------  ---------------------------------------------  --------------------------------------------  ----------------------------------------
//	Disk full during a part             setwriter.TestWriteFailingInASecondPart-       not simulated: a write error ends the         –
//	                                    RemovesAllParts (a write error after part 1;   restore like a damaged data section (bit in
//	                                    a full disk fails the same writer)             data below): INCOMPLETE
//	Part deleted                        plan.TestFoldersUseNewestCompleteFullAsBase    TestDamagedSetIsRefused/part deleted          TestDamagedSetIsRefused/part deleted
//	                                    (a broken set is never a base)
//	Part truncated                      as above                                       TestDamagedSetIsRefused/part truncated        TestDamagedSetIsRefused/part truncated
//	Bit flipped in the header           as above                                       TestDamagedSetIsRefused/bit in header         as restore
//	                                                                                   syntax, /bit in header value
//	Bit flipped in the data section     backup.TestVerifyBackupAfterWriteReports-      TestDamagedSetIsRefused/bit in data           as restore
//	                                    CorruptPart (verify after backup)
//	Bit flipped in the manifest         as above                                       TestDamagedSetIsRefused/bit in manifest       as restore
//	Bit flipped in the trailer          plan.TestFoldersUseNewestCompleteFullAsBase    TestDamagedSetIsRefused/bit in trailer        as restore
//	Base of a differential missing      health.TestBaseMissingNextBackupIsFull         restorepoint.TestProcessRestorePoint-         verify.TestVerifyPlanDescribesThe-
//	                                                                                   RejectsWrongOrMissingBase,                    Verification,
//	                                                                                   job.TestSelectionPreflightAndRows             job.TestSelectionPreflightAndRows
//	Cancel at each phase                TestCancelAtEachPhase/backup/*,                TestCancelAtEachPhase/restore/*,              TestCancelAtEachPhase/verify/*,
//	                                    TestCancelledBackupKeepsCompletedSets          TestCancelledRestoreAndVerify                 TestCancelledRestoreAndVerify
//	Source file locked                  archive.TestBuildTarFailsOnLockedFileBy-       –                                             –
//	                                    Default, archive.TestBuildTarSkipsLocked-
//	                                    FileWhenConfigured,
//	                                    TestExcludeAndUnreadableFiles
//	Source file vanishing or changing   archive.TestBuildTarFileVanishesBeforeIt-      –                                             –
//	while it is read                    IsRead, archive.TestBuildTarFileShrinks-
//	                                    WhileRead, archive.TestBuildTarFileGrows-
//	                                    WhileRead
//	Destination folder appearing        –                                              restore.TestRestoreEntryRefusesExisting-      –
//	between plan and start                                                             Destination
//	Destination folder swapped for a    –                                              archive.TestRestoreRefusesJunctionAs-         –
//	junction                                                                           Destination, archive.TestRestoreRefuses-
//	                                                                                   FolderSwappedForJunction
//
// A cancel during the cleanup is too late by design: retention deletes whole
// chains and is not stopped halfway, so the run completes
// (TestCancelAtEachPhase/backup/cleaning-up).
package e2e
