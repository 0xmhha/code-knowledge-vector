// CoreML Objective-C bridge for CKV.
// Loads an MLModel from .mlpackage/.mlmodelc and exposes C functions
// that the Go CGO layer calls.

#import <Foundation/Foundation.h>
#import <CoreML/CoreML.h>
#include "bridge.h"

static MLModel* _model = nil;

int ckv_coreml_load(const char* model_path) {
    @autoreleasepool {
        NSString* path = [NSString stringWithUTF8String:model_path];
        NSURL* url = [NSURL fileURLWithPath:path];
        NSError* error = nil;

        MLModelConfiguration* config = [[MLModelConfiguration alloc] init];
        config.computeUnits = MLComputeUnitsAll;

        _model = [MLModel modelWithContentsOfURL:url configuration:config error:&error];
        if (error != nil || _model == nil) {
            NSLog(@"ckv_coreml_load: failed to load %@: %@", path, error);
            return -1;
        }
        return 0;
    }
}

void ckv_coreml_unload(void) {
    @autoreleasepool {
        _model = nil;
    }
}
