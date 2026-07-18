LOCK TABLE
  ProductReleaseCapabilityEdge,
  ProductReleaseComponent,
  ReleaseBundle
IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM ProductReleaseCapabilityEdge)
  OR EXISTS (SELECT 1 FROM ProductReleaseComponent)
  OR EXISTS (
    SELECT 1
    FROM ReleaseBundle
    WHERE kind = 'product'
  ) THEN
    RAISE EXCEPTION
      'downgrade crossing 144 is forbidden while product release facts exist';
  END IF;
END;
$$;

DROP TABLE ProductReleaseCapabilityEdge;
DROP TABLE ProductReleaseComponent;
ALTER TABLE ReleaseBundle
  DROP CONSTRAINT releasebundle_product_version_length_check;
