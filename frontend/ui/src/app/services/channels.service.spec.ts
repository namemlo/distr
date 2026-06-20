import {provideHttpClient} from '@angular/common/http';
import {HttpTestingController, provideHttpClientTesting} from '@angular/common/http/testing';
import {TestBed} from '@angular/core/testing';
import {ChannelsService} from './channels.service';

describe('ChannelsService', () => {
  let http: HttpTestingController;
  let service: ChannelsService;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    http = TestBed.inject(HttpTestingController);
    service = TestBed.inject(ChannelsService);
  });

  afterEach(() => {
    http.verify();
  });

  it('lists channels from the channels endpoint', () => {
    service.list().subscribe((channels) => {
      expect(channels).toEqual([
        {
          id: 'channel-1',
          createdAt: '2026-06-20T09:30:00Z',
          updatedAt: '2026-06-20T10:45:00Z',
          applicationId: 'application-1',
          lifecycleId: 'lifecycle-1',
          name: 'Stable',
          description: 'Default production-ready channel',
          sortOrder: 10,
          isDefault: true,
        },
      ]);
    });

    const req = http.expectOne('/api/v1/channels');
    expect(req.request.method).toBe('GET');
    req.flush([
      {
        id: 'channel-1',
        createdAt: '2026-06-20T09:30:00Z',
        updatedAt: '2026-06-20T10:45:00Z',
        applicationId: 'application-1',
        lifecycleId: 'lifecycle-1',
        name: 'Stable',
        description: 'Default production-ready channel',
        sortOrder: 10,
        isDefault: true,
      },
    ]);
  });

  it('creates, updates, and deletes channels', () => {
    const request = {
      applicationId: 'application-1',
      lifecycleId: 'lifecycle-1',
      name: 'Stable',
      description: 'Default production-ready channel',
      sortOrder: 10,
      isDefault: true,
    };

    service.create(request).subscribe();
    const createReq = http.expectOne('/api/v1/channels');
    expect(createReq.request.method).toBe('POST');
    expect(createReq.request.body).toEqual(request);
    createReq.flush({id: 'channel-1', ...request});

    service.update('channel-1', {...request, name: 'Hotfix'}).subscribe();
    const updateReq = http.expectOne('/api/v1/channels/channel-1');
    expect(updateReq.request.method).toBe('PUT');
    expect(updateReq.request.body.name).toBe('Hotfix');
    updateReq.flush({id: 'channel-1', ...request, name: 'Hotfix'});

    service.delete('channel-1').subscribe();
    const deleteReq = http.expectOne('/api/v1/channels/channel-1');
    expect(deleteReq.request.method).toBe('DELETE');
    deleteReq.flush(null);
  });
});
