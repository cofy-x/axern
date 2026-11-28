ALTER TABLE allocation_access_grants
    DROP CONSTRAINT allocation_access_grants_purpose_check,
    ADD CONSTRAINT allocation_access_grants_purpose_check
        CHECK (purpose IN (
            'ALLOCATION_ACCESS_PURPOSE_INTERACTIVE',
            'ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT',
            'ALLOCATION_ACCESS_PURPOSE_HTTP_PREVIEW'
        ));
