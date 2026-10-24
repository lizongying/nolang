package build

// astWriteInventorySeed is the frozen AST-write inventory asserted by
// TestASTWriteInventoryPinned. It was measured on 2026-10-20 over the
// astWriteCorpus by TestASTWriteCensus (which prints it verbatim).
//
// Only the (node type, field) KEY SET is pinned — not the counts: several
// passes (mergedGlobalConsts "first occurrence wins", std worklist order) are
// order-dependent, so the number of writes legitimately varies between runs
// while the set of touched fields does not.
//
// Update rules (mirrors the C8 / §19.5 spec-pin discipline):
//   - A key DISAPPEARED → a write site was removed (S5 progress). Update the
//     pin and record the removal in docs/design/nolang-parser-lowering-
//     invariants.md §13.5.
//   - A key APPEARED → either a new AST write was introduced (a §1.4
//     violation to fix) or an existing site started firing on the corpus
//     (extend the corpus, do not silence it).
//
// S5 history (30 → 0 keys):
//   - 30 → 29: *parser.LetStatement.IsModuleConst → Program.Sem (S5 stage 2).
//   - 29 → 18: *parser.{LetStatement,FunctionDefinition}.{SourceFile,ModuleOwner}
//     and the seven other *Statement.SourceFile sites → Program.Sem
//     (S5 stage 3). Eleven keys removed, no key added.
//   - 18 → 16: *parser.FunctionDefinition.{GenericUnion,VariadicUnion} →
//     Program.Sem (S5 stage 4; the checker's ValidateUnionTypes marks union
//     monomorphisation on the side table now). Two keys removed, none added.
//   - 16 → 15: *parser.LetStatement.Type (the assembly-time effective-type
//     OVERRIDE written by retypesFactoryLets / devirtualizeInterfaceVars) →
//     Program.Sem.TypeOverride (S5 stage 5). One key removed, none added.
//   - 15 → 0: S5 stage 6 (2026-10-21) — assembly working-copy isolation.
//     parser.CloneProgram deep-copies the tree (Sem keys remapped) at every
//     merge entry; ALL remaining write sites (rename / module-type-prefix /
//     call-rewrite / const-prop families) now mutate assembly-owned copies,
//     never ParseProgram outputs. The gate's meaning hardens: ANY future write
//     reaching a parse output immediately reappears here as a new key.
var astWriteInventorySeed = []string{}
