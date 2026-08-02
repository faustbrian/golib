package faultinject

import (
	"context"
	"io/fs"
)

// WrapFS returns an fs.FS adapter with separate open and read operation
// identities. Open failures close any file already acquired; successful files
// remain caller-owned.
func WrapFS(base fs.FS, injector *Injector, openOperation, readOperation uint32) (fs.FS, error) {
	if base == nil {
		return nil, invalid("FS", "must be non-nil")
	}
	if injector == nil || !injector.enabled {
		return base, nil
	}
	return &injectedFS{
		base: base, injector: injector,
		openOperation: openOperation, readOperation: readOperation,
	}, nil
}

type injectedFS struct {
	base          fs.FS
	injector      *Injector
	openOperation uint32
	readOperation uint32
}

func (filesystem *injectedFS) Open(name string) (fs.File, error) {
	decision := filesystem.injector.Decide(Metadata{
		Boundary: BoundaryFilesystemOpen, Operation: filesystem.openOperation,
	})
	if err := faultPhaseError(context.Background(), decision.faults, PhaseBefore, filesystem.injector.sleeper); err != nil {
		return nil, err
	}
	if err := faultPhaseError(context.Background(), decision.faults, PhaseDuring, filesystem.injector.sleeper); err != nil {
		return nil, err
	}
	file, organicError := filesystem.base.Open(name)
	if err := faultPhaseError(context.Background(), decision.faults, PhaseAfter, filesystem.injector.sleeper); err != nil {
		closeFile(file)
		return nil, err
	}
	if organicError != nil || file == nil {
		return file, organicError
	}
	return &injectedFile{
		File: file,
		reader: &injectedReader{
			reader: file, injector: filesystem.injector,
			operation: filesystem.readOperation, boundary: BoundaryFilesystemRead,
		},
	}, nil
}

type injectedFile struct {
	fs.File
	reader *injectedReader
}

func (file *injectedFile) Read(buffer []byte) (int, error) {
	return file.reader.Read(buffer)
}

func closeFile(file fs.File) {
	if file != nil {
		_ = file.Close()
	}
}
