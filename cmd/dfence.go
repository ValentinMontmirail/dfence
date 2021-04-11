package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/chavacava/dfence/internal/infra"
	"github.com/chavacava/dfence/internal/policy"
	"golang.org/x/tools/go/packages"

	"github.com/fatih/color"
)

func main() {
	policyFile := flag.String("policy", "dfence.json", "the policy file to enforce")
	logLevel := flag.String("log", "error", "log level: none, error, warn, info, debug")
	mode := flag.String("mode", "check", "run mode (check or info)")
	flag.Parse()

	logger := buildlogger(*logLevel)

	var err error
	stream, err := os.Open(*policyFile)
	if err != nil {
		logger.Fatalf("Unable to open policy file %s: %+v", *policyFile, err)
	}

	policy, err := policy.NewPolicyFromJSON(stream)
	if err != nil {
		logger.Fatalf("Unable to load policy : %v", err) // revive:disable-line:deep-exit
	}

	const pkgSelector = "./..."
	logger.Infof("Retrieving packages...")
	pkgs, err := scanPackages([]string{pkgSelector}, logger)

	if err != nil {
		logger.Fatalf("Unable to retrieve packages using the selector '%s': %v", pkgSelector, err)
	}

	logger.Infof("Will work with %d package(s).", len(pkgs))

	var execErr error
	switch *mode {
	case "check":
		execErr = check(policy, pkgs, logger)
	case "info":
		execErr = info(policy, pkgs, logger)
	default:
		logger.Fatalf("Unknown mode %q, valid modes are check and info", *mode)
	}

	if execErr != nil {
		logger.Errorf(execErr.Error())
		os.Exit(1)
	}
}

func check(p policy.Policy, pkgs []*packages.Package, logger infra.Logger) error {
	checker, err := policy.NewChecker(p, pkgs, logger)
	if err != nil {
		logger.Fatalf("Unable to run the checker: %v", err)
	}

	pkgCount := len(pkgs)
	errCount := 0
	out := make(chan policy.CheckResult, pkgCount)
	for _, pkg := range pkgs {
		go checker.CheckPkg(pkg, out)
	}

	logger.Infof("Checking...")

	for i := 0; i < pkgCount; i++ {
		result := <-out
		for _, w := range result.Warns {
			logger.Warningf(w.Error())
		}
		for _, e := range result.Errs {
			logger.Errorf(e.Error())
		}

		errCount += len(result.Errs)
	}

	logger.Infof("Check done")

	if errCount > 0 {
		return fmt.Errorf("found %d error(s)", errCount)
	}

	return nil
}

func info(p policy.Policy, pkgs []*packages.Package, logger infra.Logger) error {
	for _, pkg := range pkgs {
		pkgName := pkg.PkgPath
		cs := p.GetApplicableConstraints(pkgName)
		if len(cs) == 0 {
			logger.Warningf("No constraints for %s", pkgName)
			continue
		}
		logger.Infof("Constraints for %s:", pkgName)
		for _, c := range cs {
			for _, l := range strings.Split(c.String(), "\n") {
				logger.Infof("\t%+v", l)
			}
			logger.Infof("")
		}
	}

	return nil
}

func buildlogger(level string) infra.Logger {
	nop := func(string, ...interface{}) {}
	debug, info, warn, err := nop, nop, nop, nop
	switch level {
	case "none":
		// do nothing
	case "debug":
		debug = buildLoggerFunc("[DEBUG]\t", color.New(color.FgCyan))
		fallthrough
	case "info":
		info = buildLoggerFunc("[INFO]\t", color.New(color.FgGreen))
		fallthrough
	case "warn":
		warn = buildLoggerFunc("[WARN]\t", color.New(color.FgHiYellow))
		fallthrough
	default:
		err = buildLoggerFunc("[ERROR]\t", color.New(color.FgHiRed))
	}

	fatal := buildLoggerFunc("[FATAL]\t", color.New(color.BgRed))
	return infra.NewLogger(debug, info, warn, err, fatal)
}

func buildLoggerFunc(prefix string, c *color.Color) infra.LoggerFunc {
	return func(msg string, vars ...interface{}) {
		fmt.Println(c.Sprintf(prefix+msg, vars...))
	}
}

func scanPackages(args []string, logger infra.Logger) ([]*packages.Package, error) {
	var emptyResult = []*packages.Package{}

	// Load packages and their dependencies.
	config := &packages.Config{
		Mode: packages.NeedName | packages.NeedImports | packages.NeedDeps | packages.NeedModule | packages.NeedFiles,
	}

	initial, err := packages.Load(config, args...)
	if err != nil {
		return emptyResult, fmt.Errorf("error loading packages: %w", err)
	}

	nerrs := 0
	for _, p := range initial {
		for _, err := range p.Errors {
			logger.Errorf(err.Msg)
			nerrs++
		}
	}

	if nerrs > 0 {
		return emptyResult, fmt.Errorf("failed to load initial packages. Ensure this command works first:\n\t$ go list %s", strings.Join(args, " "))
	}

	return initial, nil
}
