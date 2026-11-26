NO      ?= ../../bin/no
SQLITE  ?= /usr/bin/sqlite3

.PHONY: test-nosql vet fixture-check

test-nosql:
	$(NO) build --check lib.no
	$(NO) vet .
	$(NO) test -timeout 120 tests
	$(MAKE) fixture-check SQLITE="$(SQLITE)"

vet:
	$(NO) vet .

fixture-check:
	@version="$$($(SQLITE) --version)"; \
	case "$$version" in \
		3.54.0\ *) ;; \
		*) echo "fixture-check requires canonical SQLite 3.54.0, got: $$version" >&2; exit 1 ;; \
	esac
	@mkdir -p ../../tmp/nosql-fixtures
	$(SQLITE) :memory: '.read tests/fixtures/stage1.sql' > ../../tmp/nosql-fixtures/stage1.actual
	diff -u tests/fixtures/stage1.expected ../../tmp/nosql-fixtures/stage1.actual
	@echo "CANONICAL STAGE 1 FIXTURE PASSED"
