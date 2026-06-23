import {HttpClient} from '@angular/common/http';
import {inject, Injectable} from '@angular/core';
import {Observable} from 'rxjs';
import {
  ConfigAsCodeAuthority,
  ConfigAsCodeAuthorityListResponse,
  ConfigAsCodeAuthorityUpdateRequest,
  ConfigAsCodeResourceKind,
  ConfigAsCodeValidateRequest,
  ConfigAsCodeValidateResponse,
} from '../types/config-as-code';

const baseUrl = '/api/v1/config-as-code';

@Injectable({
  providedIn: 'root',
})
export class ConfigAsCodeService {
  private readonly httpClient = inject(HttpClient);

  validate(request: ConfigAsCodeValidateRequest): Observable<ConfigAsCodeValidateResponse> {
    return this.httpClient.post<ConfigAsCodeValidateResponse>(`${baseUrl}/validate`, request);
  }

  listAuthorities(): Observable<ConfigAsCodeAuthorityListResponse> {
    return this.httpClient.get<ConfigAsCodeAuthorityListResponse>(`${baseUrl}/authorities`);
  }

  getAuthority(kind: ConfigAsCodeResourceKind, resourceId: string): Observable<ConfigAsCodeAuthority> {
    return this.httpClient.get<ConfigAsCodeAuthority>(`${baseUrl}/authorities/${kind}/${resourceId}`);
  }

  updateAuthority(
    kind: ConfigAsCodeResourceKind,
    resourceId: string,
    request: ConfigAsCodeAuthorityUpdateRequest
  ): Observable<ConfigAsCodeAuthority> {
    return this.httpClient.put<ConfigAsCodeAuthority>(`${baseUrl}/authorities/${kind}/${resourceId}`, request);
  }
}
