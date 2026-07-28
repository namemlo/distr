import {HttpErrorResponse} from '@angular/common/http';
import {ComponentFixture, TestBed} from '@angular/core/testing';
import {provideRouter} from '@angular/router';
import {of, Subject, throwError} from 'rxjs';
import {vi} from 'vitest';
import {OperatorControlPlaneService} from '../../services/operator-control-plane.service';
import {OperatorCampaignRow, OperatorPage} from '../../types/operator-control-plane';
import {CampaignsComponent} from './campaigns.component';

describe('CampaignsComponent', () => {
  let fixture: ComponentFixture<CampaignsComponent>;
  let service: {listCampaigns: ReturnType<typeof vi.fn>};

  beforeEach(() => {
    service = {listCampaigns: vi.fn()};
    TestBed.configureTestingModule({
      imports: [CampaignsComponent],
      providers: [provideRouter([]), {provide: OperatorControlPlaneService, useValue: service}],
    });
  });

  it('shows loading until the first campaign page arrives', () => {
    service.listCampaigns.mockReturnValue(new Subject<OperatorPage<OperatorCampaignRow>>());

    fixture = TestBed.createComponent(CampaignsComponent);
    fixture.detectChanges();

    expect(text()).toContain('Loading campaigns');
  });

  it('renders draft, immutable revision, run, counts, and unknown status without inventing comparison', async () => {
    service.listCampaigns.mockReturnValue(
      of({
        items: [
          campaign({
            id: 'campaign-1',
            name: 'Payments canary',
            status: '',
            revisionId: 'revision-1',
            runId: 'run-1',
          }),
        ],
        total: 1,
      })
    );

    await createComponent();

    for (const value of [
      'Payments canary',
      'Draft draft-1',
      'Revision revision-1',
      'Run run-1',
      'Unknown',
      '2 waves',
      '3 members',
    ]) {
      expect(text()).toContain(value);
    }
    expect(text()).not.toContain('Compare');
  });

  it('distinguishes empty, forbidden, and failed list states', async () => {
    service.listCampaigns.mockReturnValue(of({items: []}));
    await createComponent();
    expect(text()).toContain('No campaigns match these filters');

    fixture.destroy();
    service.listCampaigns.mockReturnValue(
      throwError(() => new HttpErrorResponse({status: 403, statusText: 'Forbidden'}))
    );
    await createComponent();
    expect(text()).toContain('You do not have access to deployment campaigns');

    fixture.destroy();
    service.listCampaigns.mockReturnValue(
      throwError(() => new HttpErrorResponse({status: 404, statusText: 'Not Found'}))
    );
    await createComponent();
    expect(text()).toContain('Campaign control plane is not available');

    fixture.destroy();
    service.listCampaigns.mockReturnValue(
      throwError(() => new HttpErrorResponse({status: 500, statusText: 'Server Error'}))
    );
    await createComponent();
    expect(text()).toContain('Campaigns could not be loaded');
  });

  it('loads the next cursor and preserves a partial list when load more fails', async () => {
    service.listCampaigns
      .mockReturnValueOnce(of({items: [campaign()], nextCursor: 'cursor-2', total: 2}))
      .mockReturnValueOnce(throwError(() => new HttpErrorResponse({status: 503, statusText: 'Unavailable'})));
    await createComponent();

    button('Load more').click();
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    expect(service.listCampaigns.mock.calls).toEqual([[{limit: 25}], [{limit: 25, cursor: 'cursor-2'}]]);
    expect(text()).toContain('Payments canary');
    expect(text()).toContain('Some campaigns could not be loaded');
    expect(text()).toContain('Retry load more');
  });

  async function createComponent(): Promise<void> {
    fixture = TestBed.createComponent(CampaignsComponent);
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
  }

  function text(): string {
    return fixture.nativeElement.textContent as string;
  }

  function button(label: string): HTMLButtonElement {
    const buttons = Array.from(fixture.nativeElement.querySelectorAll('button')) as HTMLButtonElement[];
    const result = buttons.find((candidate) => candidate.textContent?.trim() === label);
    if (!result) throw new Error(`Missing button: ${label}`);
    return result;
  }
});

function campaign(overrides: Partial<OperatorCampaignRow> = {}): OperatorCampaignRow {
  return {
    id: 'campaign-1',
    createdAt: '2026-07-28T01:00:00Z',
    draftId: 'draft-1',
    name: 'Payments canary',
    status: 'RUNNING',
    canonicalChecksum: `sha256:${'a'.repeat(64)}`,
    waveCount: 2,
    memberCount: 3,
    pendingCount: 1,
    runningCount: 1,
    succeededCount: 1,
    failedCount: 0,
    blockedCount: 0,
    ...overrides,
  };
}
