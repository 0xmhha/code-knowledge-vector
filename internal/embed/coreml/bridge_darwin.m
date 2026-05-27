// CoreML Objective-C bridge for CKV.
// Loads an MLModel from .mlpackage/.mlmodelc and runs embedding inference.
// The Go CGO layer calls these C functions.

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

int ckv_coreml_predict(
    const int64_t* input_ids,
    const int64_t* attention_mask,
    int batch_size,
    int seq_len,
    int hidden_dim,
    float* output
) {
    @autoreleasepool {
        if (_model == nil) {
            NSLog(@"ckv_coreml_predict: no model loaded");
            return -1;
        }

        NSError* error = nil;

        // Create MLMultiArray for input_ids [batch_size, seq_len]
        NSArray<NSNumber*>* shape = @[@(batch_size), @(seq_len)];
        NSArray<NSNumber*>* strides = @[@(seq_len), @1];

        MLMultiArray* idsArray = [[MLMultiArray alloc]
            initWithDataPointer:(void*)input_ids
            shape:shape
            dataType:MLMultiArrayDataTypeInt32
            strides:strides
            deallocator:nil
            error:&error];
        if (error) {
            NSLog(@"ckv_coreml_predict: input_ids array: %@", error);
            return -2;
        }

        MLMultiArray* maskArray = [[MLMultiArray alloc]
            initWithDataPointer:(void*)attention_mask
            shape:shape
            dataType:MLMultiArrayDataTypeInt32
            strides:strides
            deallocator:nil
            error:&error];
        if (error) {
            NSLog(@"ckv_coreml_predict: attention_mask array: %@", error);
            return -3;
        }

        // Build feature provider from input arrays.
        // Model input names vary; common: "input_ids", "attention_mask".
        NSDictionary* features = @{
            @"input_ids": idsArray,
            @"attention_mask": maskArray
        };
        MLDictionaryFeatureProvider* provider =
            [[MLDictionaryFeatureProvider alloc] initWithDictionary:features error:&error];
        if (error) {
            NSLog(@"ckv_coreml_predict: feature provider: %@", error);
            return -4;
        }

        // Run prediction
        id<MLFeatureProvider> result = [_model predictionFromFeatures:provider error:&error];
        if (error || result == nil) {
            NSLog(@"ckv_coreml_predict: prediction failed: %@", error);
            return -5;
        }

        // Extract output — try common names
        MLMultiArray* outputArray = nil;
        for (NSString* name in @[@"last_hidden_state", @"output", @"embeddings"]) {
            MLFeatureValue* val = [result featureValueForName:name];
            if (val && val.type == MLFeatureTypeMultiArray) {
                outputArray = val.multiArrayValue;
                break;
            }
        }
        if (outputArray == nil) {
            NSLog(@"ckv_coreml_predict: no recognized output tensor");
            return -6;
        }

        // Copy output to the pre-allocated float buffer
        int total = batch_size * seq_len * hidden_dim;
        const float* src = (const float*)outputArray.dataPointer;
        memcpy(output, src, total * sizeof(float));

        return 0;
    }
}
